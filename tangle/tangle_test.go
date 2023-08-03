package tangle

import (
	"fmt"
	"tangle/p2p"
	"testing"
	"time"
)

var (
	peer1 = &p2p.Peer{}
	peer2 = &p2p.Peer{}
	peer3 = &p2p.Peer{}
)

func p2pNet() {
	mockAddr1 := &p2p.Address{"127.0.0.1", 8001}
	mockAddr2 := &p2p.Address{"127.0.0.1", 8002}
	mockAddr3 := &p2p.Address{"127.0.0.1", 8003}

	peer1 = p2p.NewPeer(mockAddr1.IP, mockAddr1.Port)
	peer2 = p2p.NewPeer(mockAddr2.IP, mockAddr2.Port)
	peer3 = p2p.NewPeer(mockAddr3.IP, mockAddr3.Port)

	time.Sleep(1 * time.Second)

	peer1.LookUpOthers([]*p2p.Address{mockAddr2, mockAddr3})
	peer2.LookUpOthers([]*p2p.Address{mockAddr1, mockAddr3})
	peer3.LookUpOthers([]*p2p.Address{mockAddr1, mockAddr2})

	time.Sleep(1 * time.Second)
}

func TestTangle(t *testing.T) {
	p2pNet()

	tangle1 := NewTangle(10, 4*time.Second, peer1)
	tangle2 := NewTangle(10, 4*time.Second, peer2)
	tangle3 := NewTangle(10, 4*time.Second, peer3)

	// go tangle1.ReadMsgFromP2PPool()
	// go tangle2.ReadMsgFromP2PPool()
	go tangle3.ReadMsgFromP2PPool()

	// go tangle1.UpdateTipSet()
	// go tangle2.UpdateTipSet()
	go tangle3.UpdateTipSet()

	tangle1.PublishTransaction("tx1")
	time.Sleep(10 * time.Second)

	for tipID, _ := range tangle3.TipSet {
		fmt.Printf("--------------tip TxID : %x----------------\n", tipID)
	}

	for candidateID, candidate := range tangle3.CandidateTips {
		for _, approveTx := range candidate.ApproveTx {
			fmt.Printf("--------------------candidate TxID : %x  ,  approve TxID: %x----------------------\n", candidateID, approveTx)
		}

	}

	fmt.Println()

	tangle1.PublishTransaction("tx2")
	tangle2.PublishTransaction("tx2")
	_ = tangle3
	time.Sleep(10 * time.Second)

	for tipID, _ := range tangle3.TipSet {
		fmt.Printf("--------------tip TxID : %x----------------\n", tipID)
	}

	for candidateID, candidate := range tangle3.CandidateTips {
		for _, approveTx := range candidate.ApproveTx {
			fmt.Printf("--------------------candidate TxID : %x  ,  approve TxID: %x----------------------\n", candidateID, approveTx)
		}
	}

	// tangle1.PublishTransaction("tx3")
	// tangle2.PublishTransaction("tx3")
	// tangle3.PublishTransaction("tx3")
	// time.Sleep(5 * time.Second)

	//for hash, msg := range peer2.MessagePool {
	//	fmt.Printf("节点2消息池中消息 (hash:%x) (内容:%s)\n", hash, msg.BackPayload())
	//
	//	rawTx := new(RawTransaction)
	//	if err := json.Unmarshal([]byte(msg.BackPayload().(string)), rawTx); err != nil {
	//		loglogrus.Log.Warnf("[Tangle] 无法将Payload字节流解析为Transaction,err=%v\n", err)
	//		continue
	//	}
	//	tx := &Transaction{RawTx: rawTx}
	//	txSet := make([]*Transaction, 0)
	//	txSet = append(txSet, tx)
	//
	//	tangle2.DealRcvTransaction(txSet)
	//
	//	msg.MarkRetrieved()
	//}
	//
	//for hash, msg := range peer3.MessagePool {
	//	fmt.Printf("节点3消息池中消息 (hash:%x) (内容:%s)\n", hash, msg.BackPayload())
	//	msg.MarkRetrieved()
	//}
}
