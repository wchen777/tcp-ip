package tcp

import (
	"bytes"
	"encoding/binary"
	"log"

	"github.com/google/netstack/tcpip/header"
)

// Both socket types will need a close, so pass in socket instead
// then we can call getTye to get the exact object
func (t *TCPHandler) Close(socketData *SocketData, vc *VTCPConn) error {
	// STEPS:
	// send a FIN to receiver
	tcbEntry := t.SocketTable[vc.SocketTableKey]
	tcbEntry.TCBLock.Lock()

	// According to Ed post, each packet reaching the ESTABLISHED state should have the ACK flag set
	tcpHeader := CreateTCPHeader(t.LocalAddr, socketData.DestAddr, socketData.LocalPort, socketData.DestPort,
		tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagFin|header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})

	// prevent any more sending
	tcbEntry.Cancelled.Store(true)
	// signal potentially blocked send routine to exit
	tcbEntry.SND.SendBlockedCond.Signal()
	// signal a potentally blocked write to exit
	tcbEntry.SND.WriteBlockedCond.Broadcast()

	// send FIN packet
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, t.LocalAddr)
	binary.Write(buf, binary.BigEndian, socketData.DestAddr)
	bufToSend := buf.Bytes()
	bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)

	t.IPLayerChannel <- bufToSend

	// Go into FIN WAIT 1 if ACTIVE CLOSE, otherwise go into LAST ACK for PASSIVE CLOSE
	if tcbEntry.State == CLOSE_WAIT {
		tcbEntry.State = LAST_ACK
	} else {
		tcbEntry.State = FIN_WAIT_1
	}

	tcbEntry.SND.NXT += 1
	tcbEntry.TCBLock.Unlock()

	for {
		// wait to get notification that we are in FIN WAIT 2 or in CLOSED
		// depending on active vs. passive close
		select {
		case <-tcbEntry.ReceiveChan:
			delete(t.SocketTable, *socketData)
			return nil
		}
	}
}

// close just the listener socket, which means we can't take anymore pending connections
func (t *TCPHandler) CloseListener(vl *VTCPListener) error {
	// set the cancelled value for the listener to be true
	// the accept call will drop the new connection if the cancelled value is true instead of processing it
	log.Print("cancelling the listener")
	vl.Cancelled.Store(true)

	// signal the pending conn cond in accept if it is waiting on a new connection
	t.SocketTable[vl.SocketTableKey].PendingConnMutex.Lock()
	t.SocketTable[vl.SocketTableKey].PendingConnCond.Signal()
	t.SocketTable[vl.SocketTableKey].PendingConnMutex.Unlock()

	// delete the listener from the socket table
	delete(t.SocketTable, vl.SocketTableKey)
	return nil
}
