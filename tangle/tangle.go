package tangle

import (
	"fmt"
	"sync"
	"tangle/common"
	"tangle/database"
	loglogrus "tangle/log_logrus"
	"tangle/message"
	"tangle/p2p"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

type Tangel struct {
	peer          *p2p.Peer
	Database      database.Database
	DatabaseMutex sync.RWMutex
	L0            int           // tip节点的数量
	λ             int           // 交易的生成速率(达不到的话就用空交易)
	h             time.Duration // 交易从生成到确认的时间间隔

	GenesisTx   *Transaction
	TangleGraph *Transaction // 以Genesis为根节点的有向图结构

	TipSet        map[common.Hash]bool            // 存储当前的所有Tip(key值是交易哈希值)
	CandidateTips map[common.Hash]*RawTransaction // 候选Tip交易

	curTipMutex       sync.RWMutex
	candidateTipMutex sync.RWMutex
}

func NewTangle(λ int, h time.Duration, peer *p2p.Peer) *Tangel {
	tangle := &Tangel{
		peer: peer,
		λ:    λ,
		h:    h,
	}
	if memDB1, err := leveldb.Open(storage.NewMemStorage(), nil); err != nil {
		loglogrus.Log.Errorf("当前节点(%s:%d)无法创建内存数据库,err:%v\n", peer.LocalAddr.IP, peer.LocalAddr.Port, err)
		return nil
	} else {
		tangle.Database = database.NewSimpleLDB("transaction", memDB1)
	}

	// 将创始交易存入数据库
	genesis := NewGenesisTx(common.NodeID{})
	tangle.DatabaseMutex.Lock()
	key := genesis.RawTx.TxID[:]
	value := TransactionSerialize(genesis.RawTx)
	tangle.Database.Put(key, value)
	tangle.DatabaseMutex.Unlock()

	tangle.L0 = 2 * λ * int(h.Seconds())

	tangle.GenesisTx = genesis
	tangle.TangleGraph = genesis
	tangle.TipSet = make(map[common.Hash]bool)
	tangle.TipSet[genesis.RawTx.TxID] = true
	tangle.CandidateTips = make(map[common.Hash]*RawTransaction)

	return tangle
}

func (tg *Tangel) ReadMsgFromP2PPool() {
	cycle := time.NewTicker(tg.h / 2)
	for {
		select {
		case <-cycle.C:
			allMsg := tg.peer.BackAllMsg()
			fmt.Printf("[Tangle] 当前节点(%s:%d)p2p消息池中的消息数量: %d\n", tg.peer.LocalAddr.IP, tg.peer.LocalAddr.Port, len(allMsg))

			txSet := make([]*Transaction, 0)
			for _, msg := range allMsg {
				switch msg.MsgType() {
				case message.CommonCode:
					txJsonStr := msg.BackPayload().(string)
					if tx := DecodeTxFromJsonStr(txJsonStr); tx != nil {
						txSet = append(txSet, tx)
					}
					msg.MarkRetrieved()
				}
			}
			go tg.DealRcvTransaction(txSet)
		default:
			continue
		}
	}
}

// 负责处理接收到的来自于其他节点发布的tangle交易（1.验证Pow   2.合法交易加入到CandidateTips）
func (tg *Tangel) DealRcvTransaction(txs []*Transaction) {
	validTxs := make([]*Transaction, 0) // 存储所有有效的交易(能通过Pow验证)
	for _, tx := range txs {
		if tx.PowValidator() {
			loglogrus.Log.Infof("[Tangle] 当前节点(%s:%d)(NodeID:%x)完成对来自Node(%x)交易(%x)的Pow验证\n", tg.peer.LocalAddr.IP,
				tg.peer.LocalAddr.Port, tg.peer.BackNodeID(), tx.RawTx.Sender, tx.RawTx.TxID)
			validTxs = append(validTxs, tx)
		}
	}

	tg.candidateTipMutex.Lock()
	for _, validTx := range validTxs {
		tg.CandidateTips[validTx.RawTx.TxID] = validTx.RawTx
	}

	tg.candidateTipMutex.Unlock()
}

// 定期使用candidate更新tangle的tip集合(建立在一种特殊的tip策略上：一个新生成的区块只有经历固定的时间长度后才能成为tip)
func (tg *Tangel) UpdateTipSet() {
	cycle := time.NewTicker(tg.h / 2)

	for {
		select {
		case <-cycle.C:
			now := time.Now().UnixNano()
			tg.curTipMutex.Lock()
			tg.candidateTipMutex.Lock()
			for _, candidate := range tg.CandidateTips {
				if uint64(now)-candidate.TimeStamp > uint64(tg.h.Nanoseconds()) { // 交易可以被确认(也即是可以真正上链)

					loglogrus.Log.Infof("[Tangle] 当前节点(%s:%d) 的 candidate 交易(%x)可以进行上链,变为 Tip 交易, len(PreviousTxs) = %d\n",
						tg.peer.LocalAddr.IP, tg.peer.LocalAddr.Port, candidate.TxID, len(candidate.PreviousTxs))
					prvTxs := candidate.PreviousTxs // 获取到此交易的前置交易集合

					for _, prvTx := range prvTxs { // 如果前置交易出现在 tangle.TipSet 中，则用当前交易将其替换掉
						if _, ok := tg.TipSet[prvTx]; ok {
							delete(tg.TipSet, prvTx)
						}
						tg.TipSet[candidate.TxID] = true
						// candidate作为新的tip,可以上链了
						tg.DatabaseMutex.Lock()
						key := candidate.TxID[:]
						value := TransactionSerialize(candidate)
						tg.Database.Put(key, value)
						tg.DatabaseMutex.Unlock()
						loglogrus.Log.Infof("[Tangle] 当前节点(%s:%d)成功更新tip\n", tg.peer.LocalAddr.IP, tg.peer.LocalAddr.Port)
						delete(tg.CandidateTips, candidate.TxID) // 将此上链的交易从候选tip集合中删除
					}
				}
			}
			tg.curTipMutex.Unlock()
			tg.candidateTipMutex.Unlock()
		}
	}

}

// 发布一笔交易
func (tg *Tangel) PublishTransaction(data interface{}) {
	if len(tg.TipSet) == 1 && tg.TipSet[tg.GenesisTx.RawTx.TxID] { // 当前节点的tangle结构中只有一个创世交易

		newTx := NewTransaction(data, []common.Hash{tg.GenesisTx.RawTx.TxID}, tg.peer.BackNodeID())
		tg.DatabaseMutex.Lock()
		newTx.SelectApproveTx(tg.Database)
		tg.DatabaseMutex.Unlock()
		newTx.Pow()

		// 需要将该交易广播出去
		wrapMsg := EncodeTxToWrapMsg(newTx, tg.peer.BackPrvKey())
		tg.peer.Broadcast(wrapMsg)

		tg.candidateTipMutex.Lock()
		tg.CandidateTips[newTx.RawTx.TxID] = newTx.RawTx // 交易加入到候选tip集合
		tg.candidateTipMutex.Unlock()

		return
	}

	tg.curTipMutex.RLock()
	tipSet := make([]common.Hash, 0)
	for tip, _ := range tg.TipSet {
		tipSet = append(tipSet, tip)
	}
	tg.curTipMutex.RUnlock()

	newTx := NewTransaction(data, tipSet, tg.peer.BackNodeID())

	tg.DatabaseMutex.Lock()
	newTx.SelectApproveTx(tg.Database)
	tg.DatabaseMutex.Unlock()
	newTx.Pow()

	// 需要将该交易广播出去
	wrapMsg := EncodeTxToWrapMsg(newTx, tg.peer.BackPrvKey())
	tg.peer.Broadcast(wrapMsg)

	tg.candidateTipMutex.Lock()
	tg.CandidateTips[newTx.RawTx.TxID] = newTx.RawTx // 交易加入到候选tip集合
	tg.candidateTipMutex.Unlock()
}
