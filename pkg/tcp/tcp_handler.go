package tcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"sync"
	"tcp-ip/pkg/ip"

	"github.com/google/netstack/tcpip/header"
)

type SocketData struct {
	LocalAddr uint32
	LocalPort uint16
	DestAddr  uint32
	DestPort  uint16
}

type TCPPacket struct {
	Header header.TCPFields
	Data   []byte
}

const (
	MAX_BUF_SIZE = (1 << 16) - 1 // buffer size
	MSS_DATA     = 1360          // maximum segment size of the data in the packet, 1400 - TCP_HEADER_SIZE - IP_HEADER_SIZE
)

// The Send struct and Receive structs will be used to implement sliding window
// TODO: need some pointer to indicate the next place where the application reads/writes to
/*
1         2          3          4
----------|----------|----------|----------
		  UNA       NXT         UNA + WND (boundary of what is allowed in the current window)
*/
// All of the indices here will be an absolute sequence number that we will mod by 2^16 to
// get the index within the buffer
type Send struct {
	Buffer           []byte
	UNA              uint32    // oldest unacknowledged segment
	NXT              uint32    // next byte to send
	WND              uint32    // window size as received from the receiver
	ISS              uint32    // initial send sequence number
	LBW              uint32    // last byte TO BE written
	WriteBlockedCond sync.Cond // for whether or not there is space left in the buffer to be written to from the application
	SendLock         sync.Mutex
}

/*
1         2          3
----------|----------|----------
		RCV.NXT    RCV.NXT
				  +RCV.WND
*/
// smaller size --> two separate sizes (circular + sequence space) additional pointers --> buffer vs. sequence space
type Receive struct {
	Buffer          []byte
	NXT             uint32    // the next sequence number expected to receive
	WND             uint32    // advertised window size
	IRS             uint32    // initial receive sequence number
	LBR             uint32    // keeps track of the next byte TO BE read
	ReadBlockedCond sync.Cond // for whether or not there is stuff to read in
	ReceiveLock     sync.Mutex
}

type TCB struct {
	ConnectionType int
	State          ConnectionState
	ReceiveChan    chan SocketData // some sort of channel when receiving another message on this layer
	SND            *Send
	RCV            *Receive
	TCBLock        sync.Mutex // TODO: figure out if this is necessary, is it possible that two different goroutines could be using the state variable for instance?
	ListenKey      SocketData // keeps track of the listener to properly notify the listener
	// TODO:
	// pointer to retransmit queue and current segment

	// Pending connections, for when a SYN is sent but the listener is not listening
	PendingConnCond    sync.Cond
	PendingConnMutex   sync.Mutex
	PendingConnections []SocketData
}

/*
	the TCP Handler represents the kernel's TCP stack to handle the incoming/outgoing packets to the machine
	TCP socket API calls wrap TCP handler calls, which keep track of the TCP socket's states etc.

	there is 1 TCP handler shared between all the sockets.
*/
type TCPHandler struct {
	SocketTable     map[SocketData]*TCB // maps tuple to representation of the socket
	IPLayerChannel  chan []byte         // connect with the host layer about TCP packet
	SocketTableLock sync.Mutex          // TODO: figure out if this is necessary
	LocalAddr       uint32
	CurrentPort     uint16
	Listeners       []SocketData // store current listeners so we can check to see if we're connecting to a listener
}

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

	// add to the socket table
	t.SocketTable[socketData] = newTCBEntry

	// change the state
	newTCBEntry.State = SYN_SENT
	newTCBEntry.SND.ISS = NewISS()
	newTCBEntry.SND.NXT = newTCBEntry.SND.ISS + 1
	newTCBEntry.SND.LBW = newTCBEntry.SND.NXT
	newTCBEntry.SND.UNA = newTCBEntry.SND.ISS

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
			tcbEntry.TCBLock.Unlock()
			if !readAll {
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
	// two cases:
	// error case is when it's greater than NXT and
	seqNumReceived := tcpHeader.SequenceNumber()

	// we've already ACK'd this packet, drop
	if seqNumReceived < tcbEntry.RCV.NXT {
		log.Print("Dropping packet because we've already received it")
		return
	}
	// we've received a SEQ that is out of the range of the window and not meant for us
	if seqNumReceived >= tcbEntry.RCV.NXT+tcbEntry.RCV.WND {
		log.Printf("sequence number received: %d\n", seqNumReceived)
		log.Printf("sequence number window: %d\n", tcbEntry.RCV.WND)
		log.Printf("Sequence number is out of range of the window")
		return
	}

	if seqNumReceived == tcbEntry.RCV.NXT {
		// copy the data into the buffer
		amountToCopy := uint32(len(payload))
		for (seqNumReceived+amountToCopy)-tcbEntry.RCV.LBR == MAX_BUF_SIZE {
			// wait here until there is space available to copy the entire payload
			// into the buffer

			// TODO: is it better to wait for every iteration of the copying?
		}

		amountCopied := uint32(0)

		for amountCopied < amountToCopy {
			nxt_index := SequenceToBufferInd(tcbEntry.RCV.NXT) // next pointer relative to buffer

			remainingBufSize := MAX_BUF_SIZE - (tcbEntry.RCV.NXT - tcbEntry.RCV.LBR + 1) // maximum possible read length ()
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
		}
	}
	// either sequence number is at NXT

	// or sequence number is greater, in which case -> early arrivals queue
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
		log.Printf("WND Sending: %d\n", tcbEntry.SND.UNA)
		for tcbEntry.SND.NXT-tcbEntry.SND.UNA < tcbEntry.SND.WND {
			log.Print("Each iteration of send 2")
			for tcbEntry.SND.NXT == tcbEntry.SND.LBW { // wait for more data to be written
				log.Print("we are stuck here")
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
				payload := tcbEntry.SND.Buffer[nxt_index : nxt_index+amountToCopy]

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
		// so if they are equal this means that we need to sleep until data has been ACKed
		for tcbEntry.SND.NXT-tcbEntry.SND.UNA == tcbEntry.SND.WND {
			tcbEntry.SND.WriteBlockedCond.Wait()
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

		if lbw_index+bytesToWrite > MAX_BUF_SIZE { // wrap-around case
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

/*
	handles when we receive a packet from the IP layer --> implements the TCP state machine diagram
*/
func (t *TCPHandler) ReceivePacket(packet ip.IPPacket, data interface{}) {
	// extract the header from the data portion of the IP packet
	log.Print("Received TCP packet")
	tcpData := packet.Data
	tcpHeaderAndData := tcpData[0:]
	tcpHeader := header.TCP(tcpHeaderAndData)
	tcpPayload := tcpHeaderAndData[tcpHeader.DataOffset():]

	// need to figure out which socket that this data corresponds to
	// follow the state machine for when we receive a packet
	srcPort := tcpHeader.DestinationPort()
	destPort := tcpHeader.SourcePort()

	// get the addresses from the IP layer
	localAddr := binary.BigEndian.Uint32(packet.Header.Dst.To4())
	destAddr := binary.BigEndian.Uint32(packet.Header.Src.To4())

	// check the table for the connection
	key := SocketData{LocalAddr: localAddr, LocalPort: srcPort, DestAddr: destAddr, DestPort: destPort}
	listenerKey := SocketData{LocalAddr: localAddr, LocalPort: srcPort, DestAddr: 0, DestPort: 0}

	// if the key for the listener exists and there isn't a key for the established conn, it is a listener
	if _, exists := t.SocketTable[key]; !exists {
		if _, exists := t.SocketTable[listenerKey]; exists {
			key = listenerKey
		}
	}

	log.Printf("socket data structure after receiving: %v\n", key)
	if tcbEntry, exists := t.SocketTable[key]; exists {
		// connection exists
		// TODO: call appropriate function for each state
		// Switch on the state of the receiving socket once a packet is received
		// This is implemented from Section 3.10
		switch tcbEntry.State {
		case CLOSED:
			log.Printf("received a segment when state is CLOSED")
			return
		case LISTEN:
			// first check the SYN
			log.Printf("received a segment when state is in LISTEN")
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
				newTCBEntry.State = SYN_RECEIVED              // move to SYN_RECEIVED
				newTCBEntry.ListenKey = key
				newTCBEntry.SND.WriteBlockedCond = *sync.NewCond(&newTCBEntry.TCBLock)
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
		case SYN_SENT: // looking to get a SYN ACK to acknowledge our SYN and try and SYN with us
			log.Printf("received a segment when state is in SYN_SENT")
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

					tcbEntry.ReceiveChan <- key // signal the socket that is waiting for accepts to proceed with returning a new socket

					t.IPLayerChannel <- bufToSend // send this to the ip layer channel so it can be sent as data
					go t.Send(&key, tcbEntry)
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
		case SYN_RECEIVED:
			log.Printf("received a segment when state is in SYN_RECEIVED")
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

					go t.Send(&key, tcbEntry)

					// send to accept
					// TODO: temporary solution is just adding an additional field to store the listener reference
					listenerEntry := t.SocketTable[tcbEntry.ListenKey]
					listenerEntry.PendingConnections = append(listenerEntry.PendingConnections, key)
					listenerEntry.PendingConnMutex.Lock()
					listenerEntry.PendingConnCond.Signal()
					listenerEntry.PendingConnMutex.Unlock()
				}
			} else {
				return
			}
		case ESTABLISHED:
			log.Printf("received a segment when state is in ESTABLISHED")
			// call a function or have a function that's always running?
			t.Receive(tcpHeader, tcpPayload, &key, tcbEntry)
		case FIN_WAIT_1:
		case FIN_WAIT_2:
		case CLOSING:
		case TIME_WAIT:
		case CLOSE_WAIT:
		case LAST_ACK:
		}
	} else {
		// connection does not exist
		// TODO: what to do here, do we treat the packet as effectively dropped?
		return
	}
}

func (t *TCPHandler) InitHandler(data []interface{}) {

	//if len(data) != 3 { TODO: populate this when we know the length of the data args
	//	log.Print("Incorrect length of data, returning from init handler")
	//	return
	//}

	t.SocketTable = make(map[SocketData]*TCB)

}
