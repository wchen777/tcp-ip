package tcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"github.com/google/netstack/tcpip/header"
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

	// TODO: how to get the local address and local port?
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
	newTCBEntry := &TCB{
		State:       SYN_SENT,
		ReceiveChan: make(chan SocketData), // some sort of channel when receiving another message on this layer
		SND:         &Send{Buffer: make([]byte, MAX_BUF_SIZE)},
		RCV:         &Receive{Buffer: make([]byte, MAX_BUF_SIZE)}}

	newTCBEntry.SND.WriteBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
	newTCBEntry.SND.SendBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
	newTCBEntry.RCV.ReadBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
	newTCBEntry.SND.ZeroBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)

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
	tcpHeader := header.TCPFields{
		SrcPort:       socketData.LocalPort,
		DstPort:       port,
		SeqNum:        newTCBEntry.SND.ISS,
		AckNum:        0,
		DataOffset:    20,
		Flags:         header.TCPFlagSyn,
		WindowSize:    65535,
		Checksum:      0,
		UrgentPointer: 0,
	}

	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, t.LocalAddr)
	binary.Write(buf, binary.BigEndian, destAddr)
	bufToSend := buf.Bytes()

	bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, destAddr)...)
	// tcpBytes := make(header.TCP, header.TCPMinimumSize)
	// tcpBytes.Encode(&tcpHeader)

	// bytesArray := &bytes.Buffer{}
	// binary.Write(bytesArray, binary.BigEndian, destAddr)
	// buf := bytesArray.Bytes()
	// buf = append(buf, tcpBytes...)

	// send to IP layer
	log.Print("sending tcp to channel")
	t.IPLayerChannel <- bufToSend

	// TODO: I think we should wait for a channel update here. RFC makes it seems like we should handle state changes on receive

	for {
		select {
		case err := <-t.IPErrorChannel:
			return nil, err
		case _ = <-newTCBEntry.ReceiveChan:
			return newConn, nil
		}
	}

	// return the new VTCPConn object
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
	// TODO: check if port is already used + how do we know which interface to listen on?
	if port <= 1024 {
		return nil, errors.New("Invalid port number")
	}
	// initialize tcb with listen state, return listener socket conn
	// go into the LISTEN state
	log.Print(t)
	socketTableKey := SocketData{LocalAddr: t.LocalAddr, LocalPort: port, DestAddr: 0, DestPort: 0}
	t.Listeners = append(t.Listeners, socketTableKey)
	log.Printf("socket data structure before sending: %v\n", socketTableKey)
	if _, exists := t.SocketTable[socketTableKey]; exists {
		return nil, errors.New("Listener socket already exists")
	}

	// if the entry doesn't yet exist
	listenerSocket := &VTCPListener{SocketTableKey: socketTableKey, TCPHandler: t} // the listener socket that is returned to the user
	newTCBEntry := &TCB{                                                           // start a TCB entry on the listen state to represent the listen socket
		State:       LISTEN,
		ReceiveChan: make(chan SocketData),
	}
	newTCBEntry.PendingConnCond = *sync.NewCond(&newTCBEntry.PendingConnMutex)
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
		return &VTCPConn{SocketTableKey: connEntry, TCPHandler: t}, nil
	}

	listenerTCBEntry.PendingConnMutex.Lock()
	for len(listenerTCBEntry.PendingConnections) == 0 {
		listenerTCBEntry.PendingConnCond.Wait()
	}
	connEntry := listenerTCBEntry.PendingConnections[0]
	listenerTCBEntry.PendingConnections = listenerTCBEntry.PendingConnections[1:]
	defer listenerTCBEntry.PendingConnMutex.Unlock()
	return &VTCPConn{SocketTableKey: connEntry, TCPHandler: t}, nil
}

// corresponds to RECEIVE in RFC
func (t *TCPHandler) Read(data []byte, amountToRead uint32, readAll bool, vc *VTCPConn) (uint32, error) {
	// check to see if the connection exists in the table, return error if it doesn't exist
	var tcbEntry *TCB

	// get tcb associated with the connection socket
	if val, exists := t.SocketTable[vc.SocketTableKey]; !exists {
		return 0, errors.New("Connection doesn't exist")
	} else {
		tcbEntry = val
	}

	amountReadSoFar := uint32(0)
	for amountReadSoFar < amountToRead {

		tcbEntry.TCBLock.Lock()
		if tcbEntry.RCV.LBR == tcbEntry.RCV.NXT {
			// TODO: is it okay if readAll isn't specified and we return 0 bytes read?
			// 		 not sure what it means to return with at least 1 byte read
			if !readAll {
				tcbEntry.TCBLock.Unlock()
				return amountReadSoFar, nil // there's nothing to read, so we just return in the case of a non-blocking read
			} else {
				// wait until we can get more data to read until amountToRead
				for tcbEntry.RCV.LBR == tcbEntry.RCV.NXT {
					tcbEntry.RCV.ReadBlockedCond.Wait()
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
		amountReadSoFar += bytesToRead

		tcbEntry.TCBLock.Unlock()
	}

	return amountReadSoFar, nil
}

// func (t *TCPHandler) HandleTimeouts() {

// }

// always running go routine that is receiving data from the channel

// Get the sequence number to figure out where to copy the data into the buffer
// copy data into buffer starting from NXT (presumably), while also taking into account the wrapping around

// If there is no more space remaining in the buffer, then block until data has been
// read by the application layer (i.e. when NXT - LBR == MAX_BUF_SIZE)

// Send ACKs back to confirm what we expect next
// This routine only handles ONE packet at a time

// TODO: need to include a early arrival queue
func (t *TCPHandler) Receive(tcpHeader header.TCP, payload []byte, socketData *SocketData, tcbEntry *TCB) {
	if (tcpHeader.Flags() & header.TCPFlagAck) == 0 {
		return
	}

	tcbEntry.TCBLock.Lock()
	defer tcbEntry.TCBLock.Unlock()

	// two cases:
	// error case is when it's greater than NXT and
	seqNumReceived := tcpHeader.SequenceNumber()

	// we've already ACK'd this packet, drop
	if seqNumReceived < tcbEntry.RCV.NXT {
		log.Print("Dropping packet because we've already received it")
		// TODO: send back ACK just in case for duplicates?
		// if tcbEntry.RCV.WND == 0 {
		// send ACK back
		log.Print("responding to zero probing")
		buf := &bytes.Buffer{}
		binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
		binary.Write(buf, binary.BigEndian, socketData.DestAddr)

		// the ack number should be updated because tcbEntry.RCV.NXT is updated
		log.Printf("RECEIVE NXT: %d\n", tcbEntry.RCV.NXT)
		tcpHeader := CreateTCPHeader(socketData.LocalPort, socketData.DestPort, tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND)
		bufToSend := buf.Bytes()
		bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)
		log.Print("Sending ack!")
		t.IPLayerChannel <- bufToSend
		// }
		return
	}
	// we've received a SEQ that is out of the range of the window and not meant for us
	if seqNumReceived >= tcbEntry.RCV.NXT+tcbEntry.RCV.WND {
		log.Printf("sequence number received: %d\n", seqNumReceived)
		log.Printf("sequence number window: %d\n", tcbEntry.RCV.WND)
		log.Printf("sequence number is out of range of the window")
		// TODO: send ACK for zero-probing if NXT is equal to seq num?
		// TODO: should we still send ACKs here even though this is out of range?
		if tcbEntry.RCV.WND == 0 {
			// send ACK back
			log.Print("responding to zero probing")
			buf := &bytes.Buffer{}
			binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
			binary.Write(buf, binary.BigEndian, socketData.DestAddr)

			// the ack number should be updated because tcbEntry.RCV.NXT is updated
			log.Printf("RECEIVE NXT: %d\n", tcbEntry.RCV.NXT)
			tcpHeader := CreateTCPHeader(socketData.LocalPort, socketData.DestPort, tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND)
			bufToSend := buf.Bytes()
			bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)
			log.Print("Sending ack!")
			t.IPLayerChannel <- bufToSend
		}
		return
	}

	// FOR THE RECEIVER and incrementing the NXT pointer
	// either sequence number is at NXT
	if seqNumReceived == tcbEntry.RCV.NXT {
		// copy the data into the buffer
		amountToCopy := uint32(len(payload))
		log.Printf("Amount of data received: %d\n", amountToCopy)
		amountCopied := uint32(0)

		if amountToCopy > 0 {
			for amountCopied < amountToCopy {
				nxt_index := SequenceToBufferInd(tcbEntry.RCV.NXT) // next pointer relative to buffer

				remainingBufSize := MAX_BUF_SIZE - (tcbEntry.RCV.NXT - tcbEntry.RCV.LBR) // maximum possible read length ()
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
				tcbEntry.RCV.NXT += toCopy
				tcbEntry.RCV.WND -= toCopy
			}
			buf := &bytes.Buffer{}
			binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
			binary.Write(buf, binary.BigEndian, socketData.DestAddr)

			// the ack number should be updated because tcbEntry.RCV.NXT is updated
			log.Printf("RECEIVE NXT: %d\n", tcbEntry.RCV.NXT)
			tcpHeader := CreateTCPHeader(socketData.LocalPort, socketData.DestPort, tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND)
			bufToSend := buf.Bytes()
			bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)
			log.Print("Sending ack!")
			t.IPLayerChannel <- bufToSend
		}
	} else {
		// or sequence number is greater, in which case -> early arrivals queue
	}

	tcbEntry.RCV.ReadBlockedCond.Signal()

	// FOR THE SENDER on receiving ACKS from the RECEIVER
	ackNum := tcpHeader.AckNumber()

	log.Printf("Ack num: %d\n", ackNum)
	log.Printf("Window size : %d\n", uint32(tcpHeader.WindowSize()))
	if tcpHeader.WindowSize() == 0 { // if the received advertized window size fell to 0, and the ack is out range, drop the packet
		if ackNum <= tcbEntry.SND.UNA || ackNum > tcbEntry.SND.NXT {
			return
		}
	} else if ackNum <= tcbEntry.SND.UNA || (ackNum > tcbEntry.SND.UNA+tcbEntry.SND.WND && tcbEntry.SND.WND != 0) {
		// otherwise we want to consider the case where we zero-probe
		// and the NXT hasn't yet been updated with the correct value but the new window size has been received,
		// so we use the window size to determine if the ACK num falls in range
		log.Print("Invalid sequence number, out of window / too old")
		log.Printf("WND size: %d\n", uint32(tcpHeader.WindowSize()))
		log.Printf("NXT: %d\n", tcbEntry.SND.NXT)
		log.Printf("UNA: %d\n", tcbEntry.SND.UNA)
		return
	}
	tcbEntry.SND.WND = uint32(tcpHeader.WindowSize()) // update the advertised window size given this info
	tcbEntry.SND.UNA = ackNum                         // set the last unacknowledged byte to be what the receiver has yet to acknowledge
	tcbEntry.SND.WriteBlockedCond.Signal()
	if tcpHeader.WindowSize() > 0 {
		log.Print("Signaling because the window size is no longer 0")
		tcbEntry.SND.ZeroBlockedCond.Signal()
	}
}

// always running go routine that is sending data and checking to see if the buffer is empty
// waiting on a condition variable that will be notified in write

// Check to see if distance between UNA and NXT < the window size, and then send up to
// (UNA + WINDOW_SIZE) - NXT bytes of data and increment the NXT pointer, unless NXT
// is limited by LBW

// If NXT == LBW, then wait until there is data that has been written

// TODO: add each segment to a queue for retransmission

// Sequence number should increment depending on the segment size
func (t *TCPHandler) Send(socketData *SocketData, tcbEntry *TCB) {

	// TODO: while window is 0, zero probe

	// loop condition -> while NXT - UNA < WIN (we keep sending until the window size is reached)
	for {
		log.Print("Each iteration of send 1")
		tcbEntry.TCBLock.Lock()
		log.Printf("NXT Sending: %d\n", tcbEntry.SND.NXT)
		log.Printf("UNA Sending: %d\n", tcbEntry.SND.UNA)
		log.Printf("WND Sending: %d\n", tcbEntry.SND.WND)

		for tcbEntry.SND.NXT-tcbEntry.SND.UNA < tcbEntry.SND.WND {
			log.Print("Each iteration of send 2")
			log.Printf("NXT prior to sending: %d\n", tcbEntry.SND.NXT)
			log.Printf("LBW prior to sending: %d\n", tcbEntry.SND.LBW)

			for tcbEntry.SND.NXT == tcbEntry.SND.LBW { // wait for more data to be written
				// checking this before each iterating of sending
				tcbEntry.SND.SendBlockedCond.Wait()
				log.Print("No longer waiting here because data has been written")
			}

			// how much we can send, which is constrainted by the window size starting from UNA
			totalWinIndex := tcbEntry.SND.UNA + tcbEntry.SND.WND
			amountToSend := Min(totalWinIndex-tcbEntry.SND.NXT, tcbEntry.SND.LBW-tcbEntry.SND.NXT)

			amountSent := uint32(0)

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
				tcpHeader := CreateTCPHeader(socketData.LocalPort, socketData.DestPort, tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND)
				bufToSend := buf.Bytes()
				bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)
				bufToSend = append(bufToSend, payload...)

				t.IPLayerChannel <- bufToSend

				// increment next pointer
				amountSent += amountToCopy
				tcbEntry.SND.NXT += amountToCopy
			}
		}

		for tcbEntry.SND.NXT == tcbEntry.SND.LBW { // wait for more data to be written
			// we need to make sure that there's actually data in the event that the window is zero
			tcbEntry.SND.SendBlockedCond.Wait()
			log.Print("No longer waiting here because data has been written")
		}

		if tcbEntry.SND.WND == 0 && tcbEntry.SND.LBW > tcbEntry.SND.NXT {
			cancelChan := make(chan bool)
			go func() {
				// timer and we send at each clock tick
				timer := time.NewTicker(5 * time.Second)
				for {
					select {
					case <-timer.C:
						log.Print("sending zero packet!")
						buf := &bytes.Buffer{}
						binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
						binary.Write(buf, binary.BigEndian, socketData.DestAddr)
						log.Print("sending next byte of data for zero probing")
						tcpHeader := CreateTCPHeader(socketData.LocalPort, socketData.DestPort, tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck, tcbEntry.RCV.WND)
						bufToSend := buf.Bytes()
						bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, socketData.DestAddr)...)
						nxt_index := SequenceToBufferInd(tcbEntry.SND.NXT)
						payload := []byte{tcbEntry.SND.Buffer[nxt_index]}
						bufToSend = append(bufToSend, payload...)
						t.IPLayerChannel <- bufToSend
					case <-cancelChan:
						return
					}
				}
			}()

			for tcbEntry.SND.WND == 0 {
				log.Printf("blocking here because the window is 0\n")
				tcbEntry.SND.ZeroBlockedCond.Wait()
			}

			tcbEntry.SND.NXT += 1
			cancelChan <- true
		}

		// so if they are equal this means that we need to sleep until data has been ACKed
		for tcbEntry.SND.NXT-tcbEntry.SND.UNA == tcbEntry.SND.WND && tcbEntry.SND.WND != 0 {
			// we've reached the maximum window size, this doesn't necessarily mean that the receive
			// window is zero, it's just that we can't send any more data until earlier "entries" have been ACKed
			log.Print("blocked here")
			log.Printf("window size: %d\n", tcbEntry.SND.WND)
			tcbEntry.SND.WriteBlockedCond.Wait()
			log.Print("unblocked here")
		}
		tcbEntry.TCBLock.Unlock()
	}
}

// corresponds to SEND in RFC
func (t *TCPHandler) Write(data []byte, vc *VTCPConn) (uint32, error) {
	// check to see if the connection exists in the table, return error if it doesn't exist
	var tcbEntry *TCB

	if val, exists := t.SocketTable[vc.SocketTableKey]; !exists {
		return 0, errors.New("Connection doesn't exist")
	} else {
		tcbEntry = val
	}

	// TODO: add checking for what state the entry is in
	if tcbEntry.State != ESTABLISHED {
		return 0, errors.New("Connection is closing")
	}

	amountToWrite := uint32(len(data))
	// can be greater than window size
	// payload size also can't be greater than MTU - sizeof(IP_header) - sizeof(TCP_hader) = 1360 bytes
	amountWritten := uint32(0)
	for amountWritten < amountToWrite {
		log.Printf("amount written: %d\n", amountWritten)
		tcbEntry.TCBLock.Lock()

		// there is no space to write anything, then we should block until we can
		for tcbEntry.SND.LBW-tcbEntry.SND.UNA == MAX_BUF_SIZE {
			// This should unblock from receiving ACKs
			log.Print("Waiting for space to be available in the buffer")
			tcbEntry.SND.WriteBlockedCond.Wait()
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

		// TODO: signal to the sender that there is data
		tcbEntry.SND.SendBlockedCond.Signal()
		tcbEntry.TCBLock.Unlock()

	}

	return amountToWrite, nil
}

func (t *TCPHandler) Shutdown(sdType int, vc *VTCPConn) error {
	return nil
}

// Both socket types will need a close, so pass in socket instead
// then we can call getTye to get the exact object
func (t *TCPHandler) Close(socket Socket) error {
	// TODO:
	return nil
}
