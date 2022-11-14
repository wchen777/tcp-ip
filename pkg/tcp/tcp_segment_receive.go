package tcp

import (
	"bytes"
	"encoding/binary"
	"github.com/google/netstack/tcpip/header"
	"log"
	"sync"
)

// TODO: maybe we can have a map of sorts or something
func (t *TCPHandler) HandleStateListen(tcpHeader header.TCP, localAddr uint32, srcPort uint16, destAddr uint32, destPort uint16, key *SocketData) {
	// first check the SYN
	if (tcpHeader.Flags() & header.TCPFlagAck) != 0 { // nothing should be ACK'd at this point
		return
	} else if (tcpHeader.Flags() & header.TCPFlagSyn) != 0 { // received SYN while in listen, proceed to form new conn
		// create the new socket
		// need to figure out what the local addr and local port would be
		socketData := SocketData{LocalAddr: localAddr, LocalPort: srcPort, DestAddr: destAddr, DestPort: destPort}

		// create a new tcb entry to represent the spawned socket connection
		newTCBEntry := &TCB{ConnectionType: 0,
			ReceiveChan: make(chan SocketData),
			SND:         &Send{Buffer: make([]byte, MAX_BUF_SIZE)},
			RCV:         &Receive{Buffer: make([]byte, MAX_BUF_SIZE)}}

		newTCBEntry.RCV.NXT = tcpHeader.SequenceNumber() + 1 // acknowledge the SYN's seq number (X+1)
		newTCBEntry.RCV.LBR = newTCBEntry.RCV.NXT
		newTCBEntry.RCV.IRS = tcpHeader.SequenceNumber() // start our stream at seq number (X)
		newTCBEntry.RCV.WND = MAX_BUF_SIZE
		log.Printf("window size before sending data back: %d\n", newTCBEntry.RCV.WND)
		newTCBEntry.SND.ISS = NewISS()                // start with random ISS for our SYN (= Y)
		newTCBEntry.SND.NXT = newTCBEntry.SND.ISS + 1 // next to send is (Y+1)
		newTCBEntry.SND.UNA = newTCBEntry.SND.ISS     // waiting for acknowledgement of (Y)
		newTCBEntry.SND.LBW = newTCBEntry.SND.NXT
		newTCBEntry.State = SYN_RECEIVED // move to SYN_RECEIVED
		newTCBEntry.ListenKey = *key
		newTCBEntry.SND.WriteBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
		newTCBEntry.SND.SendBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
		newTCBEntry.RCV.ReadBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
		log.Printf("adding this socket data for new entry: %v\n", socketData)
		t.SocketTable[socketData] = newTCBEntry

		// send SYN-ACK
		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, localAddr)
		binary.Write(buf, binary.BigEndian, destAddr)
		tcpHeader := CreateTCPHeader(socketData.LocalPort, destPort, newTCBEntry.SND.ISS, newTCBEntry.RCV.NXT, header.TCPFlagSyn|header.TCPFlagAck, newTCBEntry.RCV.WND)
		bufToSend := buf.Bytes()
		log.Printf("bytes to send: %v\n", bufToSend)
		bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, destAddr)...)

		// send to IP layer
		t.IPLayerChannel <- bufToSend
	} else {
		// in all other cases the packet should be dropped
		return
	}
}

func (t *TCPHandler) HandleStateSynSent(tcpHeader header.TCP, tcbEntry *TCB, localAddr uint32, srcPort uint16, destAddr uint32, destPort uint16, key *SocketData) {
	if (tcpHeader.Flags() & header.TCPFlagAck) != 0 { // received ack
		if (tcpHeader.AckNumber() <= tcbEntry.SND.ISS) || (tcpHeader.AckNumber() > tcbEntry.SND.NXT) {
			log.Printf("dropping packet") // but ack does not acknowledge our latest send
			return
		}
	}
	if (tcpHeader.Flags() & header.TCPFlagSyn) != 0 { // if syn (still can reach here if ACK is 0)
		tcbEntry.RCV.NXT = tcpHeader.SequenceNumber() + 1 // received a new seq number (Y) from SYN-ACK (orSYN) response, set next to Y+1
		// TODO: fix recv window
		tcbEntry.RCV.WND = uint32(tcpHeader.WindowSize())
		tcbEntry.SND.WND = uint32(tcpHeader.WindowSize())
		log.Printf("New window size: %d\n", tcbEntry.SND.WND)
		tcbEntry.RCV.IRS = tcpHeader.SequenceNumber() // start of client's stream is (Y)
		tcbEntry.SND.UNA = tcpHeader.AckNumber()      // the X we sent in SYN is ACK'd, set unACK'd to X++1

		if tcbEntry.SND.UNA > tcbEntry.SND.ISS {
			log.Print("reached point of being established")
			tcbEntry.State = ESTABLISHED
			// call the asynchronous send routine passing in the tcb entry that should be sending data

			tcbEntry.SND.ISS = NewISS()

			// create the tcp header with the ____
			newTCPHeader := CreateTCPHeader(srcPort, destPort, tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, MAX_BUF_SIZE)
			buf := &bytes.Buffer{}
			binary.Write(buf, binary.BigEndian, localAddr)
			binary.Write(buf, binary.BigEndian, destAddr)
			bufToSend := buf.Bytes()
			bufToSend = append(bufToSend, MarshallTCPHeader(&newTCPHeader, destAddr)...)

			tcbEntry.ReceiveChan <- *key // signal the socket that is waiting for accepts to proceed with returning a new socket

			t.IPLayerChannel <- bufToSend // send this to the ip layer channel so it can be sent as data
			go t.Send(key, tcbEntry)
			return
		}

		// (simultaneous open), ACK is 0 (just SYN)
		// enter SYN_RECEIVED
		tcbEntry.State = SYN_RECEIVED

		// need to send SYN_ACK in this case
		tcbEntry.SND.ISS = NewISS() // start with a new (Y)

		msgtcpHeader := CreateTCPHeader(srcPort, destPort, tcbEntry.SND.ISS, tcbEntry.RCV.NXT, header.TCPFlagSyn|header.TCPFlagAck, tcbEntry.RCV.WND)
		buf := MarshallTCPHeader(&msgtcpHeader, destAddr)

		t.IPLayerChannel <- buf

		return
	}
	// TODO: if the length of the payload is greater than 0, we need to do some processing?
}

func (t *TCPHandler) HandleStateSynReceived(tcpHeader header.TCP, tcbEntry *TCB, key *SocketData) {
	// how to tell if something is a passive open or not?
	// looking to get an ACK from sender
	if (tcpHeader.Flags() & header.TCPFlagSyn) != 0 { // got a SYN while in SYN_RECEIVED, need to check if passive open?
		if tcbEntry.ConnectionType == 0 {
			return
		}
	}
	if (tcpHeader.Flags() & header.TCPFlagAck) != 0 { // we received an ACK responding to our SYN-ACK
		// this ACK must acknowledge "new information" and it must be equal to (or less than) the next seq number we are trying to send
		if tcbEntry.SND.UNA < tcpHeader.AckNumber() && tcpHeader.AckNumber() <= tcbEntry.SND.NXT {
			tcbEntry.State = ESTABLISHED // move to transition to an established connect

			// call asynchronous send routine and pass in tcb entry
			tcbEntry.SND.WND = uint32(tcpHeader.WindowSize())
			log.Printf("Window size in syn received: %d\n", tcbEntry.SND.WND)

			go t.Send(key, tcbEntry)

			// send to accept
			// TODO: temporary solution is just adding an additional field to store the listener reference
			listenerEntry := t.SocketTable[tcbEntry.ListenKey]
			listenerEntry.PendingConnections = append(listenerEntry.PendingConnections, *key)
			listenerEntry.PendingConnMutex.Lock()
			listenerEntry.PendingConnCond.Signal()
			listenerEntry.PendingConnMutex.Unlock()
		}
	} else {
		return
	}
}

func (t *TCPHandler) HandleEstablished(tcpPacket TCPPacket) {

}

func (t *TCPHandler) HandleFinWait1(tcpPacket TCPPacket) {

}

func (t *TCPHandler) HandleFinWait2(tcpPacket TCPPacket) {

}

func (t *TCPHandler) HandleClosing(tcpPacket TCPPacket) {

}

func (t *TCPHandler) HandleTimeWait(tcpPacket TCPPacket) {

}

func (t *TCPHandler) HandleCloseWait(tcpPacket TCPPacket) {

}
