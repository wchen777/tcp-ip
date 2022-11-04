package tcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"log"
	"math/rand"
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

// The Send struct and Receive structs will be used to implement sliding window
// TODO: need some pointer to indicate the next place where the application reads/writes to
/*
1         2          3          4
----------|----------|----------|----------
		  UNA       NXT         UNA + WND (boundary of what is allowed in the current window)
*/
type Send struct {
	Buffer []byte
	UNA    uint32 // oldest unacknowledged segment
	NXT    uint32 // next byte to send
	Window uint32 // TODO: does this just specify the window size?
	ISS    uint32 // initial send sequence number
}

/*
1         2          3
----------|----------|----------
		RCV.NXT    RCV.NXT
				  +RCV.WND
*/
type Receive struct {
	Buffer []byte
	NXT    uint32 // the next sequence number expected to receive
	Window uint32
	IRS    uint32 // initial receive sequence number
}

type TCB struct {
	ConnectionType int
	State          ConnectionState
	ReceiveChan    chan SocketData // some sort of channel when receiving another message on this layer
	SND            *Send
	RCV            *Receive
	TCBLock        sync.Mutex // TODO: figure out if this is necessary, is it possible that two different goroutines could be using the state variable for instance?
	// TODO:
	// pointer to retransmit queue and current segment
}

type TCPHandler struct {
	SocketTable     map[SocketData]*TCB // maps tuple to representation of the socket
	IPLayerChannel  chan []byte         // connect with the host layer about TCP packet
	SocketTableLock sync.Mutex          // TODO: figure out if this is necessary
}

func CreateTCPHeader(srcPort uint16, dstPort uint16, seqNum uint32, ackNum uint32) header.TCPFields {
	return header.TCPFields{
		SrcPort:       srcPort,
		DstPort:       dstPort,
		SeqNum:        seqNum,
		AckNum:        ackNum,
		DataOffset:    20,
		Flags:         header.TCPFlagSyn | header.TCPFlagAck,
		WindowSize:    65535,
		Checksum:      0,
		UrgentPointer: 0,
	}
}

// The actual place where connect is implemented
/*
 * Creates a new socket and connects to an
 * address:port (active OPEN in the RFC).
 * Returns a VTCPConn on success or non-nil error on
 * failure.
 * VConnect MUST block until the connection is
 * established, or an error occurs.
 */
func (t *TCPHandler) Connect(addr net.IP, port uint16) (*VTCPConn, error) {
	destAddr := binary.BigEndian.Uint32(addr.To4())

	// TODO: how to get the local address and local port?
	socketData := SocketData{LocalAddr: 0, LocalPort: 0, DestAddr: destAddr, DestPort: port}

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
	}

	// add to the socket table
	t.SocketTable[socketData] = newTCBEntry

	// change the state
	newTCBEntry.State = SYN_SENT
	newTCBEntry.SND.ISS = rand.Uint32()
	newTCBEntry.SND.NXT = newTCBEntry.SND.ISS + 1
	newTCBEntry.SND.UNA = newTCBEntry.SND.ISS

	// create a SYN packet with a random sequence number
	tcpHeader := header.TCPFields{
		SrcPort:       0,
		DstPort:       port,
		SeqNum:        newTCBEntry.SND.ISS,
		AckNum:        0,
		DataOffset:    20,
		Flags:         header.TCPFlagSyn,
		WindowSize:    65535,
		Checksum:      0,
		UrgentPointer: 0,
	}
	tcpBytes := make(header.TCP, header.TCPMinimumSize)
	tcpBytes.Encode(&tcpHeader)

	bytesArray := &bytes.Buffer{}
	binary.Write(bytesArray, binary.BigEndian, destAddr)
	buf := bytesArray.Bytes()
	buf = append(buf, tcpBytes...)

	// send to IP layer
	t.IPLayerChannel <- buf

	// TODO: I think we should wait for a channel update here. RFC makes it seems like we should handle state changes on receive

	for {
		select {
		case _ = <-newTCBEntry.ReceiveChan:
			return newConn, nil
		}
	}

	// return the new VTCPConn object
	return newConn, nil
}

// Where listen is actually implemented
/*
 * Create a new listen socket bound to the specified port on any
 * of this node's interfaces.
 * After binding, this socket moves into the LISTEN state (passive
 * open in the RFC)
 *
 * Returns a TCPListener on success.  On failure, returns an
 * appropriate error in the "error" value
 */
func (t *TCPHandler) Listen(port uint16) (*VTCPListener, error) {
	// TODO: check if port is already used + how do we know which interface to listen on?

	// initialize tcb with listen state, return listener socket conn
	// go into the LISTEN state
	socketTableKey := SocketData{LocalAddr: 0, LocalPort: port, DestAddr: 0, DestPort: 0}
	if _, exists := t.SocketTable[socketTableKey]; exists {
		return nil, errors.New("Listener socket already exists")
	}

	// if the entry doesn't yet exist
	listenerSocket := &VTCPListener{SocketTableKey: socketTableKey, TCPHandler: t}
	newTCBEntry := &TCB{
		State:       LISTEN,
		ReceiveChan: make(chan SocketData),
	}
	t.SocketTable[socketTableKey] = newTCBEntry

	return listenerSocket, nil
}

// eventually each of the "socket" objects will make a call to the TCP handler stack
// can pass in the socket object that is making the call
func (t *TCPHandler) Accept(vl *VTCPListener) (*VTCPConn, error) {
	// this will block until a SYN is received from a client that's trying to connect
	var tcbEntry *TCB

	if val, exists := t.SocketTable[vl.SocketTableKey]; !exists {
		return nil, errors.New("Listener socket does not exist")
	} else {
		tcbEntry = val
	}

	for {
		select {
		case socketData := <-tcbEntry.ReceiveChan:
			// we should be in the established state at this point
			// should populate a VTCPConn
			return &VTCPConn{SocketTableKey: socketData, TCPHandler: t}, nil
		}

	}

	// create entry + return once connection is established
	return nil, nil
}

// corresponds to RECEIVE in RFC
func (t *TCPHandler) Read(data []byte, vc *VTCPConn) (int, error) {
	// check to see if the connection exists in the table, return error if it doesn't exist
	var tcbEntry *TCB

	if val, exists := t.SocketTable[vc.SocketTableKey]; !exists {
		return -1, errors.New("Connection doesn't exist")
	} else {
		tcbEntry = val
	}

	// Per section 3.9, we can check the state of the connection and handle according to the state
	switch tcbEntry.State {

	}

	return 0, nil
}

// corresponds to SEND in RFC
func (t *TCPHandler) Write(data []byte, vc *VTCPConn) (int, error) {
	// check to see if the connection exists in the table, return error if it doesn't exist
	var tcbEntry *TCB

	if val, exists := t.SocketTable[vc.SocketTableKey]; !exists {
		return -1, errors.New("Connection doesn't exist")
	} else {
		tcbEntry = val
	}

	// Per section 3.9, we can check the state of the connection and handle according to the state
	switch tcbEntry.State {

	}
	return 0, nil
}

func (t *TCPHandler) Shutdown(sdType int, vc *VTCPConn) error {
	return nil
}

// Both socket types will need a close, so pass in socket instead
// then we can call getTye to get the exact object
func (t *TCPHandler) Close(socket *Socket) error {
	return nil
}

// when we receive a packet from the IP layer
func (t *TCPHandler) ReceivePacket(packet ip.IPPacket, data interface{}) {
	// extract the header from the data portion of the IP packet
	tcpData := packet.Data
	tcpHeaderAndData := tcpData[0:packet.Header.TotalLen]
	tcpHeader := header.TCP(tcpHeaderAndData)
	// tcpPayload := tcpHeaderAndData[tcpHdr.DataOffset:]

	// need to figure out which socket that this data corresponds to
	// follow the state machine for when we receive a packet
	srcPort := tcpHeader.DestinationPort()
	destPort := tcpHeader.SourcePort()

	// get the addresses from the IP layer
	localAddr := binary.BigEndian.Uint32(packet.Header.Dst.To4())
	destAddr := binary.BigEndian.Uint32(packet.Header.Src.To4())

	// check the table for the connection
	key := SocketData{LocalAddr: localAddr, LocalPort: srcPort, DestAddr: destAddr, DestPort: destPort}
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
			if (tcpHeader.Flags() & header.TCPFlagAck) != 0 {
				return
			} else if (tcpHeader.Flags() & header.TCPFlagSyn) != 0 {
				tcbEntry.RCV.NXT = tcpHeader.SequenceNumber() + 1
				tcbEntry.RCV.IRS = tcpHeader.SequenceNumber()
				tcbEntry.State = SYN_RECEIVED

				// send SYN-ACK
				tcbEntry.SND.ISS = rand.Uint32()
				tcpHeader := header.TCPFields{
					SrcPort:       srcPort,
					DstPort:       destPort,
					SeqNum:        tcbEntry.SND.ISS,
					AckNum:        tcbEntry.RCV.NXT,
					DataOffset:    20,
					Flags:         header.TCPFlagSyn | header.TCPFlagAck,
					WindowSize:    65535,
					Checksum:      0,
					UrgentPointer: 0,
				}
				tcpBytes := make(header.TCP, header.TCPMinimumSize)
				tcpBytes.Encode(&tcpHeader)

				bytesArray := &bytes.Buffer{}
				binary.Write(bytesArray, binary.BigEndian, destAddr)
				buf := bytesArray.Bytes()
				buf = append(buf, tcpBytes...)

				// send to IP layer
				t.IPLayerChannel <- buf

				tcbEntry.SND.NXT = tcbEntry.SND.ISS + 1
				tcbEntry.SND.UNA = tcbEntry.SND.ISS
			} else {
				// in all other cases the packet should be dropped
				return
			}
		case SYN_SENT:
			// looking to get a SYN ACK
			log.Printf("received a segment when state is in SYN_SENT")
			if (tcpHeader.Flags() & header.TCPFlagAck) != 0 {
				if (tcpHeader.AckNumber() <= tcbEntry.SND.ISS) || (tcpHeader.AckNumber() > tcbEntry.SND.NXT) {
					log.Printf("dropping packet")
					return
				}
			}
			if (tcpHeader.Flags() & header.TCPFlagSyn) != 0 {
				tcbEntry.RCV.NXT = tcpHeader.SequenceNumber() + 1
				tcbEntry.RCV.IRS = tcpHeader.SequenceNumber()
				tcbEntry.SND.UNA = tcpHeader.AckNumber()

				if tcbEntry.SND.UNA > tcbEntry.SND.ISS {
					tcbEntry.State = ESTABLISHED

					// TODO: refactor this so it happens in a resusable function
					tcbEntry.SND.ISS = rand.Uint32()
					tcpHeader := header.TCPFields{
						SrcPort:       srcPort,
						DstPort:       destPort,
						SeqNum:        tcbEntry.SND.NXT,
						AckNum:        tcbEntry.RCV.NXT,
						DataOffset:    20,
						Flags:         header.TCPFlagAck,
						WindowSize:    65535,
						Checksum:      0,
						UrgentPointer: 0,
					}
					tcpBytes := make(header.TCP, header.TCPMinimumSize)
					tcpBytes.Encode(&tcpHeader)

					bytesArray := &bytes.Buffer{}
					binary.Write(bytesArray, binary.BigEndian, destAddr)
					buf := bytesArray.Bytes()
					buf = append(buf, tcpBytes...)

					tcbEntry
					t.IPLayerChannel <- buf
					return
				}

				// (simultaneous open)
				// enter SYN_RECEIVED
				tcbEntry.State = SYN_RECEIVED

				// need to send SYN_ACK in this case
				tcbEntry.SND.ISS = rand.Uint32()
				msgtcpHeader := header.TCPFields{
					SrcPort:       srcPort,
					DstPort:       destPort,
					SeqNum:        tcbEntry.SND.ISS,
					AckNum:        tcbEntry.RCV.NXT,
					DataOffset:    20,
					Flags:         header.TCPFlagSyn | header.TCPFlagAck,
					WindowSize:    65535,
					Checksum:      0,
					UrgentPointer: 0,
				}
				tcpBytes := make(header.TCP, header.TCPMinimumSize)
				tcpBytes.Encode(&msgtcpHeader)

				bytesArray := &bytes.Buffer{}
				binary.Write(bytesArray, binary.BigEndian, destAddr)
				buf := bytesArray.Bytes()
				buf = append(buf, tcpBytes...)

				t.IPLayerChannel <- buf

				tcbEntry.SND.Window = uint32(tcpHeader.WindowSize())
				return
			}
			// TODO: if the length of the payload is greater than 0, we need to do some processing?
		case SYN_RECEIVED:
			log.Printf("received a segment when state is in SYN_RECEIVED")
			// how to tell if something is a passive open or not?
			// looking to get an ACK from sender
			if (tcpHeader.Flags() & header.TCPFlagSyn) != 0 {
				if tcbEntry.ConnectionType == 0 {
					return
				}
			}
			if (tcpHeader.Flags() & header.TCPFlagAck) != 0 {
				if tcbEntry.SND.UNA < tcpHeader.AckNumber() && tcpHeader.AckNumber() <= tcbEntry.SND.NXT {
					tcbEntry.State = ESTABLISHED

					tcbEntry.SND.Window = uint32(tcpHeader.WindowSize())
				}
			} else {
				return
			}
		case ESTABLISHED:
			log.Printf("received a segment when state is in SYN")
		case FIN_WAIT_1:
		case FIN_WAIT_2:
		case CLOSING:
		case TIME_WAIT:
		case CLOSE_WAIT:
		case LAST_ACK:
		}
		tcbEntry.ReceiveChan <- tcpHeaderAndData
	} else {
		// connection does not exist
		// TODO: what to do here, do we treat the packet as effectively dropped?
		return
	}
}

func (t *TCPHandler) InitHandler(data []interface{}) {

}

func (t *TCPHandler) AddChanRoutine() {
	return
}

func (t *TCPHandler) RemoveChanRoutine() {
	return
}
