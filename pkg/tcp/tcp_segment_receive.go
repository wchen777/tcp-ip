package tcp

import (
	"bytes"
	"encoding/binary"
	"log"
	"sync"
	"time"

	"github.com/google/netstack/tcpip/header"
	"go.uber.org/atomic"
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
			ReceiveChan:         make(chan SocketData),
			SND:                 &Send{Buffer: make([]byte, MAX_BUF_SIZE)},
			RCV:                 &Receive{Buffer: make([]byte, MAX_BUF_SIZE)},
			RetransmissionQueue: make([]*RetransmitSegment, 0),
			RTOTimeoutChan:      make(chan bool),
			SegmentToTimestamp:  make(map[uint32]int64),
			RTO:                 RTO_UPPER,
			SRTT:                -1,
		}
		newTCBEntry.Cancelled = atomic.NewBool(false)
		newTCBEntry.TimeoutCancelled = atomic.NewBool(false)
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
		newTCBEntry.SND.ZeroBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
		log.Printf("adding this socket data for new entry: %v\n", socketData)
		t.SocketTable[socketData] = newTCBEntry

		// send SYN-ACK
		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, localAddr)
		binary.Write(buf, binary.BigEndian, destAddr)
		tcpHeader := CreateTCPHeader(localAddr, destAddr, socketData.LocalPort, destPort, newTCBEntry.SND.ISS,
			newTCBEntry.RCV.NXT, header.TCPFlagSyn|header.TCPFlagAck, newTCBEntry.RCV.WND, []byte{})
		bufToSend := buf.Bytes()
		log.Printf("bytes to send: %v\n", bufToSend)
		bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, destAddr)...)

		// send to IP layer
		t.IPLayerChannel <- bufToSend

		// calling this so that we can receive an ACK after sending SYNACK
		go t.WaitForAckReceiver(newTCBEntry, &socketData)
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
			newTCPHeader := CreateTCPHeader(key.LocalAddr, key.DestAddr, srcPort, destPort, tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, MAX_BUF_SIZE, []byte{})
			buf := &bytes.Buffer{}
			binary.Write(buf, binary.BigEndian, localAddr)
			binary.Write(buf, binary.BigEndian, destAddr)
			bufToSend := buf.Bytes()
			bufToSend = append(bufToSend, MarshallTCPHeader(&newTCPHeader, destAddr)...)

			tcbEntry.ReceiveChan <- *key // signal the socket that is waiting for accepts to proceed with returning a new socket

			t.IPLayerChannel <- bufToSend   // send this to the ip layer channel so it can be sent as data
			go t.Send(key, tcbEntry)        // start goroutine that sends from buffer
			go t.waitTimeout(tcbEntry, key) // start goroutine to wait for timeouts when packets are sent
			return
		}

		// (simultaneous open), ACK is 0 (just SYN)
		// enter SYN_RECEIVED
		tcbEntry.State = SYN_RECEIVED

		// need to send SYN_ACK in this case
		tcbEntry.SND.ISS = NewISS() // start with a new (Y)

		msgtcpHeader := CreateTCPHeader(key.LocalAddr, key.DestAddr, srcPort, destPort, tcbEntry.SND.ISS, tcbEntry.RCV.NXT, header.TCPFlagSyn|header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
		buf := MarshallTCPHeader(&msgtcpHeader, destAddr)

		t.IPLayerChannel <- buf

		return
	}
	// TODO: if the length of the payload is greater than 0, we need to do some processing?
}

func (t *TCPHandler) HandleStateSynReceived(tcpHeader header.TCP, tcbEntry *TCB, key *SocketData, payload []byte) {
	// we should not be receiving data in the handshake
	if len(payload) > 0 {
		return
	}

	// how to tell if something is a passive open or not?
	// looking to get an ACK from sender
	if (tcpHeader.Flags() & header.TCPFlagSyn) != 0 { // got a SYN while in SYN_RECEIVED, need to check if passive open?
		// if tcbEntry.ConnectionType == 0 {
		// 	return
		// }
		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, key.LocalAddr)
		binary.Write(buf, binary.BigEndian, key.DestAddr)
		tcpHeader := CreateTCPHeader(key.LocalAddr, key.DestAddr, key.LocalPort, key.DestPort, tcbEntry.SND.ISS,
			tcbEntry.RCV.NXT, header.TCPFlagSyn|header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
		bufToSend := buf.Bytes()

		bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, key.DestAddr)...)

		// send to IP layer
		t.IPLayerChannel <- bufToSend
	}
	if (tcpHeader.Flags() & header.TCPFlagAck) != 0 { // we received an ACK responding to our SYN-ACK
		// this ACK must acknowledge "new information" and it must be equal to (or less than) the next seq number we are trying to send
		if tcbEntry.SND.UNA < tcpHeader.AckNumber() && tcpHeader.AckNumber() <= tcbEntry.SND.NXT {
			tcbEntry.State = ESTABLISHED // move to transition to an established connect

			// call asynchronous send routine and pass in tcb entry
			tcbEntry.SND.WND = uint32(tcpHeader.WindowSize())
			log.Printf("Window size in syn received: %d\n", tcbEntry.SND.WND)

			// signal to goroutine that we have received ACK and can return
			tcbEntry.RTOTimeoutChan <- true

			go t.Send(key, tcbEntry)        // start goroutine that sends from buffer
			go t.waitTimeout(tcbEntry, key) // start goroutine to wait for timeouts when packets are sent

			// send to accept
			// TODO: temporary solution is just adding an additional field to store the listener reference
			listenerEntry := t.SocketTable[tcbEntry.ListenKey]
			// signal the pending connections queue that there is another incoming connection
			listenerEntry.PendingConnections = append(listenerEntry.PendingConnections, *key)
			listenerEntry.PendingConnMutex.Lock()
			listenerEntry.PendingConnCond.Signal()
			listenerEntry.PendingConnMutex.Unlock()
		}
	} else {
		return
	}
}

func (t *TCPHandler) HandleEstablished(tcpHeader header.TCP, tcbEntry *TCB, key *SocketData, tcpPayload []byte) {
	// TODO: check if FIN flag is set --> if so, send back an ACK for the seq num+1 and go into passive close (CLOSE WAIT)
	if (tcpHeader.Flags() & header.TCPFlagFin) != 0 {
		t.ReceiveFin(tcpHeader, key, tcbEntry) // FIN packet
	} else {
		t.Receive(tcpHeader, tcpPayload, key, tcbEntry) // receive a "normal" packet
	}
}

func (t *TCPHandler) HandleFinWait1(tcpHeader header.TCP, tcbEntry *TCB, key *SocketData, tcpPayload []byte) {
	// want to receive an ACK in FIN WAIT 1
	// TODO: need to handle simultaneous CLOSE, so if we receive a FIN, we need to ACK and change states
	tcbEntry.TCBLock.Lock()

	log.Print("In FINWAIT 1")
	if (tcpHeader.Flags() & header.TCPFlagAck) != 0 {
		log.Printf("Received ACK Num: %d\n", tcpHeader.AckNumber())
		log.Printf("NXT number to check: %d\n", tcbEntry.SND.NXT)
		if tcpHeader.AckNumber() == tcbEntry.SND.NXT { // if the ACK acknowledges our FIN packet, go into FIN WAIT 2
			log.Print("Updating state to be FIN_WAIT_2")
			tcbEntry.State = FIN_WAIT_2
			tcbEntry.TCBLock.Unlock()
			return
		}
	} else { // drop packet if no ack
		log.Printf("No ACK")
		tcbEntry.TCBLock.Unlock()
		return
	}

	if (tcpHeader.Flags() & header.TCPFlagFin) != 0 { // if there is a fin... (packet is guaranteed to have ACK here)
		if tcpHeader.AckNumber() == tcbEntry.SND.NXT-1 { // this checks that our FIN wasn't ACK'd
			// check to make sure that the ACK is correct
			// TODO: though this probably was already checked? so confusion
			tcbEntry.State = CLOSING // the other side did not ACK our FIN, so we need to wait for it
		} else {
			tcbEntry.State = TIME_WAIT // the receiver ACK'd our FIN, go straight to TIME WAIT (no more data on either side)
		}
		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, key.LocalAddr)
		binary.Write(buf, binary.BigEndian, key.DestAddr)

		tcbEntry.RCV.NXT += 1 // increment our NXT for the ACK

		tcpHdr := CreateTCPHeader(key.LocalAddr, key.DestAddr, key.LocalPort, key.DestPort,
			tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
		bufToSend := buf.Bytes()
		bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHdr, key.DestAddr)...)

		log.Print("Sending ack for FIN: FIN WAIT 1")
		t.IPLayerChannel <- bufToSend

		if tcbEntry.State == TIME_WAIT { // TIME WAIT has no more data to send, wait to close conn
			go t.WaitForClosed(tcbEntry, key)
		}
		tcbEntry.TCBLock.Unlock()
		return
	}

	tcbEntry.TCBLock.Unlock()
	// should still receive packets as normal
	t.Receive(tcpHeader, tcpPayload, key, tcbEntry)
}

func (t *TCPHandler) WaitForClosed(tcbEntry *TCB, key *SocketData) {
	// Set a timeout, and once we reach the timeout, then we set the state to be closed
	// there is a chance however, we might get another FIN and need to reset the timeout
	timeout := time.After(time.Duration(2 * time.Second * MSL))

	for {
		select {
		case <-tcbEntry.ResetWaitChan:
			// reset the timeout in the event that we get another FIN here
			timeout = time.After(time.Duration(2 * time.Second * MSL))
		case <-timeout:
			// then go into CLOSED and notify the close routine that we have reached close
			tcbEntry.TCBLock.Lock()
			tcbEntry.State = CLOSED
			tcbEntry.ReceiveChan <- *key
			tcbEntry.TCBLock.Unlock()
			return
		}
	}
}

func (t *TCPHandler) HandleFinWait2(tcpHeader header.TCP, tcbEntry *TCB, key *SocketData, tcpPayload []byte) {
	if (tcpHeader.Flags() & header.TCPFlagFin) != 0 {
		// TODO: what would the sequence number be here? Do we need to error check it?
		// Receiving a FIN, send ACK back with incremented RCV.NXT
		tcbEntry.TCBLock.Lock()

		// change state to be TIME_WAIT
		tcbEntry.State = TIME_WAIT
		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, key.LocalAddr)
		binary.Write(buf, binary.BigEndian, key.DestAddr)

		tcbEntry.RCV.NXT += 1 // increment our NXT for the ACK

		tcpHdr := CreateTCPHeader(key.LocalAddr, key.DestAddr, key.LocalPort, key.DestPort,
			tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
		bufToSend := buf.Bytes()
		bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHdr, key.DestAddr)...)

		log.Print("Sending ack for FIN: FIN WAIT 2")
		t.IPLayerChannel <- bufToSend
		tcbEntry.ResetWaitChan = make(chan bool)
		tcbEntry.TCBLock.Unlock()

		// spawn a goroutine that is waiting for the close timer
		go t.WaitForClosed(tcbEntry, key)
	} else {
		// TODO: can you still receive packets in FIN_WAIT_2?
		log.Print("Received non-fin packet in fin wait 2")
		t.Receive(tcpHeader, tcpPayload, key, tcbEntry)
	}
}

func (t *TCPHandler) HandleClosing(tcpHeader header.TCP, tcbEntry *TCB, key *SocketData) {
	tcbEntry.TCBLock.Lock()
	defer tcbEntry.TCBLock.Unlock()

	if (tcpHeader.Flags() & header.TCPFlagAck) != 0 {
		if tcpHeader.AckNumber() == tcbEntry.SND.NXT {
			tcbEntry.State = TIME_WAIT
			go t.WaitForClosed(tcbEntry, key)
		}
	}
}

func (t *TCPHandler) HandleTimeWait(tcpHeader header.TCP, tcbEntry *TCB, key *SocketData) {
	// TODO: maybe handle when we receive a SYN

	// Handle case when we receive another FIN --> need to reset 2*MSL timeout
	if (tcpHeader.Flags() & header.TCPFlagFin) != 0 {
		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, key.LocalAddr)
		binary.Write(buf, binary.BigEndian, key.DestAddr)

		tcbEntry.TCBLock.Lock()
		tcbEntry.RCV.NXT += 1 // increment our NXT for the ACK

		tcpHdr := CreateTCPHeader(key.LocalAddr, key.DestAddr, key.LocalPort, key.DestPort,
			tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
		bufToSend := buf.Bytes()
		bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHdr, key.DestAddr)...)

		log.Print("Sending ack for FIN: TIME WAIT")
		t.IPLayerChannel <- bufToSend

		// sending a flag so that we can reset the timeout in the event that we receive another FIN
		tcbEntry.ResetWaitChan <- true

		tcbEntry.TCBLock.Unlock()
	}
}

func (t *TCPHandler) HandleCloseWait(tcpHeader header.TCP, tcbEntry *TCB, key *SocketData) {
	// only possible packet received in CLOSE WAIT is a duplicate FIN if our ACK was dropped
	// if so, send back an ACK with what we expect
	if (tcpHeader.Flags() & header.TCPFlagFin) != 0 {
		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, key.LocalAddr)
		binary.Write(buf, binary.BigEndian, key.DestAddr)

		tcbEntry.TCBLock.Lock()

		tcpHdr := CreateTCPHeader(key.LocalAddr, key.DestAddr, key.LocalPort, key.DestPort,
			tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
		bufToSend := buf.Bytes()
		bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHdr, key.DestAddr)...)

		log.Print("Sending ack for old FIN: CLOSE WAIT")
		t.IPLayerChannel <- bufToSend

		tcbEntry.TCBLock.Unlock()
	}
}

func (t *TCPHandler) HandleLastAck(tcpHeader header.TCP, tcbEntry *TCB, key *SocketData) {
	// once in last ack, want to receive an ACK
	if (tcpHeader.Flags() & header.TCPFlagAck) != 0 {
		tcbEntry.TCBLock.Lock()
		// if the ACK acknowledges our FIN packet, go into CLOSED
		if tcpHeader.AckNumber() == tcbEntry.SND.NXT {
			tcbEntry.State = CLOSED
			tcbEntry.ReceiveChan <- *key // notify the applications close function that we can return
		}
		tcbEntry.TCBLock.Unlock()
	}
}
