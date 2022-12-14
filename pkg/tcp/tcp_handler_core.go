package tcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/google/netstack/tcpip/header"
	"go.uber.org/atomic"
)

// The TCP Handler Core file implements the functions for the "network stack".
// This includes goroutines for sending and receiving packets,
// as well as the underlying user functions called for send and write.

/*
* Creates a new socket and connects to an
* address:port (active OPEN in the RFC).
* Returns a VTCPConn on success or non-nil error on
* failure.
* VConnect MUST block until the connection is
* established, or an error occurs.

* called by a "client" trying to connect to a listen socket
 */
func (t *TCPHandler) Connect(addr net.IP, port uint16) (*VTCPConn, error) {
	destAddr := binary.BigEndian.Uint32(addr.To4())

	socketData := SocketData{LocalAddr: t.LocalAddr, LocalPort: t.CurrentPort, DestAddr: destAddr, DestPort: port}
	t.CurrentPort += 1

	if _, exists := t.SocketTable[socketData]; exists {
		return nil, errors.New("Socket for this address already exists")
	}

	// create a TCB entry and socketData entry once we have verified it doesn't already exist
	newConn := &VTCPConn{
		SocketTableKey: socketData,
		TCPHandler:     t,
	}
	newConn.ReadCancelled = atomic.NewBool(false)
	newConn.WriteCancelled = atomic.NewBool(false)

	newTCBEntry := &TCB{
		State:               SYN_SENT,
		ReceiveChan:         make(chan SocketData), // some sort of channel when receiving another message on this layer
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
	newTCBEntry.SND.WriteBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
	newTCBEntry.SND.SendBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
	newTCBEntry.RCV.ReadBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
	newTCBEntry.SND.ZeroBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)

	newTCBEntry.EarlyArrivalQueue = &EarlyArrivalQueue{}
	newTCBEntry.EarlyArrivalQueue.Init()

	newTCBEntry.PendingSendingFin = atomic.NewBool(false)
	newTCBEntry.PendingSendingFinCond = *sync.NewCond(&newTCBEntry.TCBLock)

	newTCBEntry.CControlEnabled = atomic.NewBool(false)
	newTCBEntry.CWND = MAX_BUF_SIZE

	// add to the socket table
	t.SocketTable[socketData] = newTCBEntry

	// change the state
	newTCBEntry.State = SYN_SENT
	newTCBEntry.SND.ISS = NewISS()
	newTCBEntry.SND.NXT = newTCBEntry.SND.ISS + 1
	newTCBEntry.SND.LBW = newTCBEntry.SND.NXT
	newTCBEntry.SND.UNA = newTCBEntry.SND.ISS
	newTCBEntry.RCV.WND = MAX_BUF_SIZE

	// create a SYN packet with a random sequence number
	tcpHeader := CreateTCPHeader(t.LocalAddr, destAddr, socketData.LocalPort, port, newTCBEntry.SND.ISS, 0, header.TCPFlagSyn, MAX_BUF_SIZE, []byte{})

	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, t.LocalAddr)
	binary.Write(buf, binary.BigEndian, destAddr)
	bufToSend := buf.Bytes()

	bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, destAddr)...)

	// send to IP layer
	// log.Print("sending tcp to channel")
	t.IPLayerChannel <- bufToSend
	timeout := time.After(RTO_UPPER)

	for {
		select {
		case <-timeout:
			// check how many times SYN has been sent, and if it's been sent numerous times,
			if newTCBEntry.RetransmitCounter == MAX_RETRANSMISSIONS {
				// log.Print("Connection timed out!")
				// make socket CLOSED
				newTCBEntry.State = CLOSED

				// remove ourselves from the socket table
				delete(t.SocketTable, socketData)
				return nil, errors.New("Connection timed out")
			}

			// increment number of times
			newTCBEntry.RetransmitCounter++

			// resend SYN
			tcpHeader := CreateTCPHeader(t.LocalAddr, destAddr, socketData.LocalPort, port, newTCBEntry.SND.ISS, 0, header.TCPFlagSyn, MAX_BUF_SIZE, []byte{})

			buf := &bytes.Buffer{}
			binary.Write(buf, binary.BigEndian, t.LocalAddr)
			binary.Write(buf, binary.BigEndian, destAddr)
			bufToSend := buf.Bytes()

			bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, destAddr)...)

			// send to IP layer
			// log.Print("sending tcp to channel for timeout")
			t.IPLayerChannel <- bufToSend

			// reset timeout after sending out packet
			timeout = time.After(RTO_UPPER)
		case err := <-t.IPErrorChannel: // propagate error if exists
			return nil, err
		case _ = <-newTCBEntry.ReceiveChan: // handshake complete, reset retransmit counter
			newTCBEntry.RetransmitCounter = 0
			// don't need to reset timeout here.
			return newConn, nil
		}
	}
}

/*
* Create a new listen socket bound to the specified port on any
* of this node's interfaces.
* After binding, this socket moves into the LISTEN state (passive
* open in the RFC)
*
* Returns a TCPListener on success.  On failure, returns an
* appropriate error in the "error" value

* called by a "server" waiting for new connections
 */
func (t *TCPHandler) Listen(port uint16) (*VTCPListener, error) {
	if port <= 1024 {
		return nil, errors.New("Invalid port number")
	}
	// initialize tcb with listen state, return listener socket conn
	// go into the LISTEN state
	// log.Print(t)
	socketTableKey := SocketData{LocalAddr: t.LocalAddr, LocalPort: port, DestAddr: 0, DestPort: 0}
	t.Listeners = append(t.Listeners, socketTableKey)
	// log.Printf("socket data structure before sending: %v\n", socketTableKey)
	if _, exists := t.SocketTable[socketTableKey]; exists {
		return nil, errors.New("Listener socket already exists")
	}

	// if the entry doesn't yet exist
	listenerSocket := &VTCPListener{SocketTableKey: socketTableKey, TCPHandler: t} // the listener socket that is returned to the user
	listenerSocket.Cancelled = atomic.NewBool(false)
	newTCBEntry := &TCB{ // start a TCB entry on the listen state to represent the listen socket
		State:       LISTEN,
		ReceiveChan: make(chan SocketData),
	}
	newTCBEntry.PendingConnCond = *sync.NewCond(&newTCBEntry.PendingConnMutex)
	newTCBEntry.CControlEnabled = atomic.NewBool(false)
	t.SocketTable[socketTableKey] = newTCBEntry

	return listenerSocket, nil
}

// eventually each of the "socket" objects will make a call to the TCP handler stack
// can pass in the socket object that is making the call
func (t *TCPHandler) Accept(vl *VTCPListener) (*VTCPConn, error) {
	// this will block until a SYN is received from a client that's trying to connect
	var listenerTCBEntry *TCB

	// get tcb entry associated with the listener socket
	if val, exists := t.SocketTable[vl.SocketTableKey]; !exists {
		return nil, errors.New("Listener socket does not exist")
	} else {
		listenerTCBEntry = val
	}

	// if we already have an existing pending connection in the queue, just pop it and return the new connection
	if len(listenerTCBEntry.PendingConnections) > 0 {
		connEntry := listenerTCBEntry.PendingConnections[0]
		listenerTCBEntry.PendingConnections = listenerTCBEntry.PendingConnections[1:]
		return &VTCPConn{SocketTableKey: connEntry, TCPHandler: t,
			ReadCancelled:  atomic.NewBool(false),
			WriteCancelled: atomic.NewBool(false)}, nil
	}

	listenerTCBEntry.PendingConnMutex.Lock()
	for len(listenerTCBEntry.PendingConnections) == 0 {
		// log.Print("blocking here")
		listenerTCBEntry.PendingConnCond.Wait()
		if vl.Cancelled.Load() == true {
			return nil, errors.New("Listener has been cancelled due to close")
		}
	}
	connEntry := listenerTCBEntry.PendingConnections[0]
	listenerTCBEntry.PendingConnections = listenerTCBEntry.PendingConnections[1:]
	defer listenerTCBEntry.PendingConnMutex.Unlock()

	return &VTCPConn{SocketTableKey: connEntry,
		TCPHandler: t, ReadCancelled: atomic.NewBool(false),
		WriteCancelled: atomic.NewBool(false)}, nil

}

// corresponds to RECEIVE in RFC
func (t *TCPHandler) Read(data []byte, amountToRead uint32, readAll bool, vc *VTCPConn) (uint32, error) {

	// check if we are in a closing state, if so -> return error
	if vc.ReadCancelled.Load() {
		// this should catch when we are in LAST_ACK
		return 0, errors.New("Read operation is not permitted")
	}

	// check to see if the connection exists in the table, return error if it doesn't exist
	var tcbEntry *TCB

	// get tcb associated with the connection socket
	if val, exists := t.SocketTable[vc.SocketTableKey]; !exists {
		return 0, errors.New("Connection doesn't exist")
	} else {
		tcbEntry = val
	}

	amountReadSoFar := uint32(0)

	tcbEntry.TCBLock.Lock()

	if tcbEntry.TimeoutCancelled.Load() {
		tcbEntry.TCBLock.Unlock()
		return 0, errors.New("Read on timed out connection")
	}

	// if the other side of the connection has closed (passive close), return EOF on read
	if tcbEntry.State == CLOSE_WAIT && tcbEntry.RCV.LBR == tcbEntry.RCV.NXT {
		tcbEntry.TCBLock.Unlock()
		return 0, io.EOF
	}

	for amountReadSoFar < amountToRead {

		if tcbEntry.RCV.LBR == tcbEntry.RCV.NXT {
			if !readAll && amountReadSoFar > 0 {
				tcbEntry.TCBLock.Unlock()
				return amountReadSoFar, nil // there's nothing to read, so we just return in the case of a non-blocking read
			} else if tcbEntry.State == CLOSE_WAIT || tcbEntry.TimeoutCancelled.Load() { // if our socket is cancelled/closed and we re-enter read with no data to read
				tcbEntry.TCBLock.Unlock()
				return 0, io.EOF
			} else {
				// wait until we can get more data to read until amountToRead
				for tcbEntry.RCV.LBR == tcbEntry.RCV.NXT {
					//log.Print("Blocked because LBR is equal NXT")
					tcbEntry.RCV.ReadBlockedCond.Wait()
					log.Print("Unblocked here")
					// check for no data + closed socket, if so, exit
					if tcbEntry.RCV.LBR == tcbEntry.RCV.NXT && (tcbEntry.State == CLOSE_WAIT || tcbEntry.TimeoutCancelled.Load()) {
						tcbEntry.TCBLock.Unlock()
						return 0, io.EOF
					}
				}
			}
		}

		var bytesToRead uint32

		// bytes to read is the minimum of what is left to read and how much there is left to read in the TCB
		leftToReadBuf := tcbEntry.RCV.NXT - tcbEntry.RCV.LBR
		if amountToRead-amountReadSoFar < leftToReadBuf {
			bytesToRead = amountToRead - amountReadSoFar
		} else {
			bytesToRead = leftToReadBuf
		}

		tcbEntry.TCBLock.Unlock()
		// LBR relative to the buffer indices
		lbr_index := SequenceToBufferInd(tcbEntry.RCV.LBR)

		if lbr_index+bytesToRead >= MAX_BUF_SIZE {
			// There shouldn't be a case during which we would wrap around the buffer twice
			// NXT - LBR should not be greater than 2^16
			// how much to copy from lbr to the end of the buffer
			copyToEndOfBufferLen := MAX_BUF_SIZE - lbr_index
			// how much to copy to the beginning of the buffer
			copyOverflowLen := bytesToRead - copyToEndOfBufferLen

			// copies from lbr to the end of the buffer
			firstCopyDataEndIndex := amountReadSoFar + copyToEndOfBufferLen // index from bytes read accumulator to the length of first copy
			copy(data[amountReadSoFar:firstCopyDataEndIndex], tcbEntry.RCV.Buffer[lbr_index:])

			// copies from the beginning of the buffer to how much was wrapped around
			secondCopyDataEndIndex := firstCopyDataEndIndex + copyOverflowLen // index from the first copy to the second copy accounting for overflowed rest of buffer
			copy(data[firstCopyDataEndIndex:secondCopyDataEndIndex], tcbEntry.RCV.Buffer[:copyOverflowLen])
		} else {
			// copy the however much we can in the buffer
			copy(data[amountReadSoFar:amountReadSoFar+bytesToRead], tcbEntry.RCV.Buffer[lbr_index:lbr_index+bytesToRead])
		}

		// increment LBR and the number of bytes read so far
		tcbEntry.RCV.LBR += bytesToRead
		tcbEntry.RCV.WND += bytesToRead
		// log.Printf("Window after reading: %d\n", tcbEntry.RCV.WND)
		amountReadSoFar += bytesToRead
		tcbEntry.TCBLock.Lock()
	}

	tcbEntry.TCBLock.Unlock()
	return amountReadSoFar, nil
}

// func (t *TCPHandler) HandleTimeouts() {

// }

// Get the sequence number to figure out where to copy the data into the buffer
// copy data into buffer starting from NXT (presumably), while also taking into account the wrapping around

// If there is no more space remaining in the buffer, then block until data has been
// read by the application layer (i.e. when NXT - LBR == MAX_BUF_SIZE)

// Send ACKs back to confirm what we expect next
// This routine only handles ONE packet at a time

func (t *TCPHandler) Receive(tcpHeader header.TCP, payload []byte, socketData *SocketData, tcbEntry *TCB) {
	if (tcpHeader.Flags() & header.TCPFlagAck) == 0 {
		return
	}

	// log.Print("before locking")
	tcbEntry.TCBLock.Lock()
	// defer tcbEntry.TCBLock.Unlock()

	// ---------------- for the RECEIVER on receiving DATA from a SENDER --------------- //

	// first, check if the packet has data, if it doesn't, then the receiver doesn't care about it.
	// (but it might be an ACK for the sender)
	// log.Print("reached receive function")
	if len(payload) > 0 {
		// log.Print("payload is greater than 0")
		// two cases:
		seqNumReceived := tcpHeader.SequenceNumber()

		// we've already ACK'd this packet, drop but send ACK
		// if the seq num is less than NXT means we've already acknowledged this packet, but the ACK may have been lost
		// sender is still timing out on this ACK, so we want to keep sending back an ACK
		if seqNumReceived < tcbEntry.RCV.NXT {
			log.Print("Dropping packet because we've already received it")
			// sending back ACK just in case for duplicates?
			buf := &bytes.Buffer{}
			binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
			binary.Write(buf, binary.BigEndian, socketData.DestAddr)

			// the ack number should be updated because tcbEntry.RCV.NXT is updated
			// log.Printf("RECEIVE NXT: %d\n", tcbEntry.RCV.NXT)
			tcpHeader := CreateTCPHeader(socketData.LocalAddr, socketData.DestAddr, socketData.LocalPort, socketData.DestPort,
				tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
			bufToSend := buf.Bytes()
			bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)
			// log.Print("Sending ack!")
			t.IPLayerChannel <- bufToSend
		} else if seqNumReceived >= tcbEntry.RCV.NXT+tcbEntry.RCV.WND {
			// we've received a SEQ that is out of the range of the window and not meant for us, still send ACK
			// log.Printf("sequence number received: %d\n", seqNumReceived)
			// log.Printf("sequence number window: %d\n", tcbEntry.RCV.WND)
			// log.Printf("sequence number is out of range of the window")
			//if tcbEntry.RCV.WND == 0 { // responding to zero-probing window -- in this case the WND is 0 so seq num will be equal to NXT
			// send ACK back w/ current nxt no matter what
			log.Print("responding to zero probing")
			buf := &bytes.Buffer{}
			binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
			binary.Write(buf, binary.BigEndian, socketData.DestAddr)

			// the ack number should be updated because tcbEntry.RCV.NXT is updated
			// log.Printf("Sending ack RECEIVE NXT: %d\n", tcbEntry.RCV.NXT)
			// log.Printf("Sending ack RECEIVE WND: %d\n", tcbEntry.RCV.WND)
			tcpHeader := CreateTCPHeader(socketData.LocalAddr, socketData.DestAddr, socketData.LocalPort, socketData.DestPort,
				tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
			bufToSend := buf.Bytes()
			bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)
			// log.Print("Sending ack!")
			t.IPLayerChannel <- bufToSend
		} else if seqNumReceived >= tcbEntry.RCV.NXT {
			// FOR THE RECEIVER and incrementing the NXT pointer

			// guaranteed to be in the window here.
			// either sequence number is at NXT, or it is greater, in which early arrivals should be set to true
			earlyArrivals := seqNumReceived > tcbEntry.RCV.NXT

			// copy the data into the buffer
			amountToCopy := uint32(len(payload))
			// log.Printf("Amount of data received: %d\n", amountToCopy)
			// log.Printf("LBR: %d\n", tcbEntry.RCV.LBR)
			// log.Printf("NXT: %d\n", tcbEntry.RCV.NXT)
			amountCopied := uint32(0)

			// TODO: when we add separate mutexes,
			// if the window size is off, truncate the window size??? ignore the other bytes, don't need to worry about
			// THIS IS BECAUSE AN OLDER WINDOW SIZE COULD HAVE BEEN ADVERTISED

			// create helper variables
			var startCopyIndex uint32 // where we should start copying into our buffer, NXT if non-early arrivals, farther into buffer if early arrivals
			if earlyArrivals {
				startCopyIndex = seqNumReceived
			} else {
				startCopyIndex = tcbEntry.RCV.NXT
			}

			for amountCopied < amountToCopy {
				nxt_index := SequenceToBufferInd(startCopyIndex) // next pointer relative to buffer

				remainingBufSize := MAX_BUF_SIZE - (startCopyIndex - tcbEntry.RCV.LBR) // maximum possible read length ()
				toCopy := Min(remainingBufSize, amountToCopy-amountCopied)

				if nxt_index+toCopy >= MAX_BUF_SIZE {
					firstCopy := MAX_BUF_SIZE - nxt_index
					copy(tcbEntry.RCV.Buffer[nxt_index:], payload[amountCopied:amountCopied+firstCopy])
					secondCopy := toCopy - firstCopy
					copy(tcbEntry.RCV.Buffer[:secondCopy], payload[amountCopied+firstCopy:amountCopied+firstCopy+secondCopy])
				} else {
					copy(tcbEntry.RCV.Buffer[nxt_index:nxt_index+toCopy], payload[amountCopied:amountCopied+toCopy])
				}

				amountCopied += toCopy

				if !earlyArrivals { // increment our NXT and pointers only if we are receiving a non-out of order packet
					tcbEntry.RCV.NXT += toCopy
					tcbEntry.RCV.WND -= toCopy
				}
				startCopyIndex += toCopy // increment our copy index for both cases
			}

			if !earlyArrivals {
				// if we received an in order packet, we need to
				// check to see if we can update the ACK num to account for previously early arrived packets
				previousRCVNXT := tcbEntry.RCV.NXT
				tcbEntry.RCV.NXT = t.updateAckNum(tcbEntry, tcbEntry.RCV.NXT)
				tcbEntry.RCV.WND -= tcbEntry.RCV.NXT - previousRCVNXT
			} else {
				// if we are receiving an early arrival packet, we should add it to the queue
				// log.Printf("Received early arrival: %d, payload length: %d\n", seqNumReceived, uint32(len(payload)))
				earlyArrivalEntry := EarlyArrivalEntry{SequenceNum: seqNumReceived, PayloadLen: uint32(len(payload))}
				tcbEntry.EarlyArrivalQueue.Push(earlyArrivalEntry)
			}

			buf := &bytes.Buffer{}
			binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
			binary.Write(buf, binary.BigEndian, socketData.DestAddr)

			// the ack number should be updated because tcbEntry.RCV.NXT is updated
			// log.Printf("Sending ACK RECEIVE NXT: %d\n", tcbEntry.RCV.NXT)
			// log.Printf("Sending ACK WND: %d\n", tcbEntry.RCV.WND)

			var nextToSend uint32

			if tcbEntry.RCV.NXT == tcbEntry.PendingReceivedFin {
				// if there's a pending FIN that we received, then we increment RCV.NXT and go into CLOSE_WAIT
				// tcbEntry.RCV.NXT += 1
				nextToSend = tcbEntry.RCV.NXT + 1
				tcbEntry.State = CLOSE_WAIT
			} else {
				nextToSend = tcbEntry.RCV.NXT
			}

			tcpHeader := CreateTCPHeader(socketData.LocalAddr, socketData.DestAddr, socketData.LocalPort, socketData.DestPort,
				tcbEntry.SND.NXT, nextToSend, header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
			bufToSend := buf.Bytes()
			bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)

			t.IPLayerChannel <- bufToSend
			// log.Print("Sending ack!")

			tcbEntry.RCV.ReadBlockedCond.Signal() //  we've read a packet, now we can try and unblock the read
		}
	}

	// ---------------- for the SENDER on receiving ACKS from a RECEIVER --------------- //
	// no data could be sent from the other side, but still want to process the sender side!
	ackNum := tcpHeader.AckNumber()

	if tcbEntry.CControlEnabled.Load() && ackNum == tcbEntry.CurrentAck {
		// we should only reach here in the event that congestion control
		// is enabled
		log.Printf("ack num received: %d\n", tcbEntry.CurrentAck)
		log.Printf("ack number frequency: %d\n", tcbEntry.CurrentAckFreq)
		if tcbEntry.CurrentAckFreq == 3 && len(tcbEntry.RetransmissionQueue) > 0 {
			// checking to see if we can retransmit
			log.Print("DUP 3 TIMES")
			tcbEntry.CurrentAckFreq = 0
			t.fastRetransmit(tcbEntry, socketData)
			return
		} else {
			// increment the frequency otherwise
			tcbEntry.CurrentAckFreq++
		}
	}

	// log.Printf("Ack num received: %d\n", ackNum)
	// log.Printf("Window size received: %d\n", uint32(tcpHeader.WindowSize()))
	if tcpHeader.WindowSize() == 0 { // if the received advertized window size fell to 0, and the ack is out range, drop the packet
		if ackNum <= tcbEntry.SND.UNA || ackNum > tcbEntry.SND.NXT {
			tcbEntry.TCBLock.Unlock()
			return
		}
	} else if ackNum <= tcbEntry.SND.UNA || (ackNum > tcbEntry.SND.UNA+tcbEntry.SND.WND && tcbEntry.SND.WND != 0) {
		// otherwise we want to consider the case where we zero-probe
		// and the NXT hasn't yet been updated with the correct value but the new window size has been received,
		// so we use the window size to determine if the ACK num falls in range
		// log.Print("Invalid sequence number, out of window / too old")
		// log.Printf("WND size: %d\n", uint32(tcpHeader.WindowSize()))
		// log.Printf("NXT: %d\n", tcbEntry.SND.NXT)
		// log.Printf("UNA: %d\n", tcbEntry.SND.UNA)
		tcbEntry.TCBLock.Unlock()
		return
	}
	// remove ACK'ed segment from the queue
	t.removeFromRetransmissionQueue(tcbEntry, ackNum)

	if tcbEntry.CControlEnabled.Load() {
		t.UpdateCWNDWindow(tcbEntry)

		// reset the ack num and the corresponding frequency
		tcbEntry.CurrentAck = ackNum
		log.Printf("Updated current ack after receiving a new one: %d\n", tcbEntry.CurrentAck)
		tcbEntry.CurrentAckFreq = 1
	}

	// call cacluate SRTT + RTO function
	tcbEntry.SND.WND = uint32(tcpHeader.WindowSize()) // update the advertised window size given this info
	tcbEntry.SND.UNA = ackNum                         // set the last unacknowledged byte to be what the receiver has yet to acknowledge
	tcbEntry.SND.WriteBlockedCond.Broadcast()         // broadcast instead since both the writer and sender rely on UNA
	if tcpHeader.WindowSize() > 0 {
		// log.Print("Signaling because the window size is no longer 0")
		tcbEntry.SND.ZeroBlockedCond.Signal()
	}
	tcbEntry.TCBLock.Unlock()
	t.updateTimeout(tcbEntry, ackNum)
	// log.Print("finished updating timeout!!")
}

// function to handle a FIN packet and going into passive close
func (t *TCPHandler) ReceiveFin(tcpHeader header.TCP, socketData *SocketData, tcbEntry *TCB) {
	// log.Print("Received FIN packet while in established")

	tcbEntry.TCBLock.Lock()
	defer tcbEntry.TCBLock.Unlock()

	// check seq num of FIN
	// if tcpHeader.SequenceNumber() < tcbEntry.RCV.NXT {
	// 	// FIN is spurious (less than ), drop FIN
	// }

	// FIN seq num is up to date with what we expect
	// log.Printf("FIN sequence number: %d\n", tcpHeader.SequenceNumber())
	// log.Printf("Receiving FIN expected NXT: %d\n", tcbEntry.RCV.NXT)

	var nxtToSend uint32

	if tcpHeader.SequenceNumber() == tcbEntry.RCV.NXT {
		// log.Print("FIN seq num is up to date")
		tcbEntry.State = CLOSE_WAIT      // go into CLOSE WAIT statea (passive close)
		nxtToSend = tcbEntry.RCV.NXT + 1 // increment ACK num for fin
		// signal a blocking read that the other side has closed the connection
		tcbEntry.RCV.ReadBlockedCond.Signal()
	} else if tcpHeader.SequenceNumber() > tcbEntry.RCV.NXT {
		// log.Print("Fin seq num is too advanced")
		// queue the pending FIN
		tcbEntry.PendingReceivedFin = tcpHeader.SequenceNumber()
		nxtToSend = tcbEntry.RCV.NXT // keep old NXT value
		// in ReceivePacket when we get the data we want
		// we will send an ACK back for the FIN and go into CLOSE_WAIT and notify reader
	}

	// send an ACK back, only if this FIN is up to date with what we expect (RCV.NXT)
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
	binary.Write(buf, binary.BigEndian, socketData.DestAddr)

	// log.Printf("RCV nxt before sending ack for fin: %d\n", tcbEntry.RCV.NXT)
	tcpHdr := CreateTCPHeader(socketData.LocalAddr, socketData.DestAddr, socketData.LocalPort, socketData.DestPort,
		tcbEntry.SND.NXT, nxtToSend, header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
	bufToSend := buf.Bytes()
	bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHdr, socketData.DestAddr)...)

	// log.Print("Sending ack for FIN")
	t.IPLayerChannel <- bufToSend
}

// always running go routine that is sending data and checking to see if the buffer is empty
// waiting on a condition variable that will be notified in write

// Check to see if distance between UNA and NXT < the window size, and then send up to
// (UNA + WINDOW_SIZE) - NXT bytes of data and increment the NXT pointer, unless NXT
// is limited by LBW

// If NXT == LBW, then wait until there is data that has been written

// Sequence number should increment depending on the segment size
func (t *TCPHandler) Send(socketData *SocketData, tcbEntry *TCB) {

	// loop condition -> while NXT - UNA < WIN (we keep sending until the window size is reached)
	for {
		// log.Print("Each iteration of send 1")
		tcbEntry.TCBLock.Lock()
		// log.Printf("NXT Sending: %d\n", tcbEntry.SND.NXT)
		// log.Printf("UNA Sending: %d\n", tcbEntry.SND.UNA)
		// log.Printf("WND Sending: %d\n", tcbEntry.SND.WND)

		for tcbEntry.SND.NXT-tcbEntry.SND.UNA < Min(tcbEntry.SND.WND, tcbEntry.CWND) {
			log.Printf("Current window size: %d\n", Min(tcbEntry.SND.WND, tcbEntry.CWND))
			// log.Print("Each iteration of send 2")
			// log.Printf("NXT prior to sending: %d\n", tcbEntry.SND.NXT)
			// log.Printf("LBW prior to sending: %d\n", tcbEntry.SND.LBW)

			for tcbEntry.SND.NXT == tcbEntry.SND.LBW { // wait for more data to be written
				// checking this before each iterating of sending
				if tcbEntry.PendingSendingFin.Load() { // if we have a fin that we need to send, we can now send it
					tcbEntry.PendingSendingFinCond.Signal()
					tcbEntry.TCBLock.Unlock()
					return
				}
				if tcbEntry.Cancelled.Load() { // if our socket has close called, stop sending
					tcbEntry.TCBLock.Unlock()
					return
				}
				log.Print("waiting for data to be written 1")
				tcbEntry.SND.SendBlockedCond.Wait() // wait until we get more data to send
				if tcbEntry.Cancelled.Load() {      // also check after we are done waiting for close
					tcbEntry.TCBLock.Unlock()
					return
				}
				// log.Print("No longer waiting here because data has been written 1")
			}

			// how much we can send, which is constrainted by the window size starting from UNA
			totalWinIndex := tcbEntry.SND.UNA + Min(tcbEntry.SND.WND, tcbEntry.CWND)
			amountToSend := Min(totalWinIndex-tcbEntry.SND.NXT, tcbEntry.SND.LBW-tcbEntry.SND.NXT)

			amountSent := uint32(0)

			// unlock here?
			tcbEntry.TCBLock.Unlock()

			for amountSent < amountToSend {
				// the amount we need to copy into the packet's payload is either the MSS, or whatever's left to send
				amountToCopy := Min(amountToSend-amountSent, MSS_DATA)

				// grab this amount in the send buffer as our payload
				nxt_index := SequenceToBufferInd(tcbEntry.SND.NXT)
				payload := make([]byte, 0)
				if nxt_index+amountToCopy >= MAX_BUF_SIZE {
					amountLeftover := (nxt_index + amountToCopy) - MAX_BUF_SIZE
					payload = tcbEntry.SND.Buffer[nxt_index:]
					payload = append(payload, tcbEntry.SND.Buffer[0:amountLeftover]...)
				} else {
					payload = tcbEntry.SND.Buffer[nxt_index : nxt_index+amountToCopy]
				}

				// TODO: think about how fine grained we want the TCB lock to be, i.e. should receive buffer have it's own lock

				// write our packet and send to the ip layer
				buf := &bytes.Buffer{}
				binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
				binary.Write(buf, binary.BigEndian, socketData.DestAddr)
				tcpHeader := CreateTCPHeader(socketData.LocalAddr, socketData.DestAddr, socketData.LocalPort, socketData.DestPort,
					tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND, payload)
				bufToSend := buf.Bytes()
				bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)
				bufToSend = append(bufToSend, payload...)

				if tcbEntry.Cancelled.Load() {
					return
				}

				// add segment to the end of the retransmission queue
				retransmitPacketEntry := &RetransmitSegment{
					TCPHeader:  &tcpHeader,
					PayloadLen: uint32(len(payload)),
				}
				tcbEntry.RetransmissionQueue = append(tcbEntry.RetransmissionQueue, retransmitPacketEntry)

				// log.Printf("payload length: %d\n", len(payload))
				t.IPLayerChannel <- bufToSend
				// log.Print("finished sending to ip layer")

				// increment next pointer
				amountSent += amountToCopy
				tcbEntry.SND.NXT += amountToCopy

				tcbEntry.TCBLock.Lock()
				tcbEntry.SegmentToTimestamp[tcbEntry.SND.NXT] = time.Now().UnixNano()
				tcbEntry.TCBLock.Unlock()

				// there's a small window at which we can be receiving packets here? maybe?
				tcbEntry.RTOTimeoutChan <- true
				// tcbEntry.TCBLock.Lock()
				// log.Print("finished updating the timeout in send!")
			}
			tcbEntry.TCBLock.Lock()
		}

		for tcbEntry.SND.NXT == tcbEntry.SND.LBW { // wait for more data to be written
			if tcbEntry.PendingSendingFin.Load() { // if we have a fin that we need to send, we can now send it
				tcbEntry.PendingSendingFinCond.Signal()
				tcbEntry.TCBLock.Unlock()
				return
			}
			// we need to make sure that there's actually data in the event that the window is zero
			if tcbEntry.Cancelled.Load() {
				tcbEntry.TCBLock.Unlock()
				return
			}
			log.Print("waiting for more data to be written 2")
			tcbEntry.SND.SendBlockedCond.Wait()
			if tcbEntry.Cancelled.Load() {
				tcbEntry.TCBLock.Unlock()
				return
			}
			// log.Print("No longer waiting here because data has been written 2")
		}

		if tcbEntry.SND.WND == 0 && tcbEntry.SND.LBW > tcbEntry.SND.NXT {
			cancelChan := make(chan bool)

			// zero-probing goroutine
			go func() {
				// timer and we send at each clock tick
				timer := time.NewTicker(5 * time.Second)
				for {
					select {
					case <-timer.C:
						// log.Print("sending zero packet!")
						buf := &bytes.Buffer{}
						binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
						binary.Write(buf, binary.BigEndian, socketData.DestAddr)
						// log.Print("sending next byte of data for zero probing")

						nxt_index := SequenceToBufferInd(tcbEntry.SND.NXT)
						payload := []byte{tcbEntry.SND.Buffer[nxt_index]} // try and send a single next byte zero-probe

						tcpHeader := CreateTCPHeader(socketData.LocalAddr, socketData.DestAddr, socketData.LocalPort, socketData.DestPort,
							tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND, payload)
						bufToSend := buf.Bytes()
						bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)
						bufToSend = append(bufToSend, payload...)
						if tcbEntry.Cancelled.Load() {
							tcbEntry.SND.ZeroBlockedCond.Signal()
							return
						}
						t.IPLayerChannel <- bufToSend
					case <-cancelChan:
						return
					}
				}
			}()

			// cond wait for the zero probe
			for tcbEntry.SND.WND == 0 {
				// log.Printf("blocking here because the window is 0\n")
				tcbEntry.SND.ZeroBlockedCond.Wait()
				if tcbEntry.Cancelled.Load() {
					tcbEntry.TCBLock.Unlock()
					return
				}
			}

			// once we are done zero probing, cancel the zero-probing goroutine
			tcbEntry.SND.NXT += 1
			cancelChan <- true
		}

		// so if they are equal this means that we need to sleep until data has been ACKed
		for tcbEntry.SND.NXT-tcbEntry.SND.UNA == Min(tcbEntry.SND.WND, tcbEntry.CWND) && tcbEntry.SND.WND != 0 {
			// we've reached the maximum window size, this doesn't necessarily mean that the receive
			// window is zero, it's just that we can't send any more data until earlier "entries" have been ACKed
			log.Print("blocked here")
			// log.Printf("window size: %d\n", tcbEntry.SND.WND)
			if tcbEntry.Cancelled.Load() {
				tcbEntry.TCBLock.Unlock()
				return
			}
			tcbEntry.SND.WriteBlockedCond.Wait()
			// log.Print("unblocked here")
			if tcbEntry.Cancelled.Load() {
				tcbEntry.TCBLock.Unlock()
				return
			}
		}
		tcbEntry.TCBLock.Unlock()
	}
}

// corresponds to SEND in RFC
func (t *TCPHandler) Write(data []byte, vc *VTCPConn) (uint32, error) {
	// check to see if the connection exists in the table, return error if it doesn't exist
	if vc.WriteCancelled.Load() {
		return 0, errors.New("Write operation not permitted")
	}

	var tcbEntry *TCB

	if val, exists := t.SocketTable[vc.SocketTableKey]; !exists {
		return 0, errors.New("Connection doesn't exist")
	} else {
		tcbEntry = val
	}

	if tcbEntry.TimeoutCancelled.Load() {
		return 0, errors.New("Write on timed out connection")
	}

	amountToWrite := uint32(len(data))
	// can be greater than window size
	// payload size also can't be greater than MTU - sizeof(IP_header) - sizeof(TCP_hader) = 1360 bytes
	amountWritten := uint32(0)
	for amountWritten < amountToWrite {
		tcbEntry.TCBLock.Lock()

		// check for a valid socket state
		if (tcbEntry.State != ESTABLISHED) && (tcbEntry.State != CLOSE_WAIT) {
			log.Printf("tcbEntry state: %s\n", SocketStateToString(tcbEntry.State))
			return 0, errors.New("Connection is closing")
		}

		// there is no space to write anything, then we should block until we can
		for tcbEntry.SND.LBW-tcbEntry.SND.UNA == MAX_BUF_SIZE {
			// This should unblock from receiving ACKs
			// log.Print("Waiting for space to be available in the buffer")
			tcbEntry.SND.WriteBlockedCond.Wait()

			if vc.WriteCancelled.Load() {
				tcbEntry.TCBLock.Unlock()
				return 0, errors.New("Write on a closed socket")
			}

			if tcbEntry.TimeoutCancelled.Load() {
				tcbEntry.TCBLock.Unlock()
				return 0, errors.New("Write on timed out connection")
			}

			// log.Print("There is space in the buffer now!")
		}

		remainingBufSize := MAX_BUF_SIZE - (tcbEntry.SND.LBW - tcbEntry.SND.UNA)
		// bytes to write is the minimum between the remaining buffer size and the rest we have left to write
		bytesToWrite := Min(remainingBufSize, amountToWrite-amountWritten)

		// LBW relative to buffer indices
		lbw_index := SequenceToBufferInd(tcbEntry.SND.LBW)

		if lbw_index+bytesToWrite >= MAX_BUF_SIZE { // wrap-around case
			// need to copy twice for overflow, same logic as in read.
			copyToEndOfBufferLen := MAX_BUF_SIZE - lbw_index       // length until end of buffer
			copyOverflowLen := bytesToWrite - copyToEndOfBufferLen // remaining length after ov

			firstCopyDataEndIndex := amountWritten + copyToEndOfBufferLen     // index into user's data stream after first overflow copy
			secondCopyDataEndIndex := firstCopyDataEndIndex + copyOverflowLen // index into user's data stream after second copy for the remainder

			// copy send buffer to end into data's starting offset to first copy index
			copy(tcbEntry.SND.Buffer[lbw_index:], data[amountWritten:firstCopyDataEndIndex])
			// copy from the start of the send buffer to the length of the remainder into the rest of the user's data buffer
			copy(tcbEntry.SND.Buffer[:copyOverflowLen], data[firstCopyDataEndIndex:secondCopyDataEndIndex])
		} else {
			copy(tcbEntry.SND.Buffer[lbw_index:], data[amountWritten:amountWritten+bytesToWrite])
		}

		amountWritten += bytesToWrite
		tcbEntry.SND.LBW += bytesToWrite
		// log.Printf("amount written: %d\n", amountWritten)

		// signal to the sender that there is data
		tcbEntry.SND.SendBlockedCond.Signal()
		tcbEntry.TCBLock.Unlock()

	}

	return amountToWrite, nil
}
