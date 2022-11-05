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
	ListenKey      SocketData // TODO: temporary solution
	// TODO:
	// pointer to retransmit queue and current segment
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
	socketTableKey := SocketData{LocalAddr: 0, LocalPort: port, DestAddr: 0, DestPort: 0}
	if _, exists := t.SocketTable[socketTableKey]; exists {
		return nil, errors.New("Listener socket already exists")
	}

	// if the entry doesn't yet exist
	listenerSocket := &VTCPListener{SocketTableKey: socketTableKey, TCPHandler: t} // the listener socket that is returned to the user
	newTCBEntry := &TCB{                                                           // start a TCB entry on the listen state to represent the listen socket
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

	// get tcb entry associated with the listener socket
	if val, exists := t.SocketTable[vl.SocketTableKey]; !exists {
		return nil, errors.New("Listener socket does not exist")
	} else {
		tcbEntry = val
	}

	for {
		select {
		case socketData := <-tcbEntry.ReceiveChan:
			// we should be in the established state at this point, as the tcb handler will have signaled the tcb channel
			// should populate a VTCPConn
			return &VTCPConn{SocketTableKey: socketData, TCPHandler: t}, nil
		}

	}

	// create entry + return once connection is established
}

// corresponds to RECEIVE in RFC
func (t *TCPHandler) Read(data []byte, vc *VTCPConn) (int, error) {
	// check to see if the connection exists in the table, return error if it doesn't exist
	var tcbEntry *TCB

	// get tcb associated with the connection socket
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
	// TODO:
	return nil
}

/*
	handles when we receive a packet from the IP layer --> implements the TCP state machine diagram
*/
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
			if (tcpHeader.Flags() & header.TCPFlagAck) != 0 { // nothing should be ACK'd at this point
				return
			} else if (tcpHeader.Flags() & header.TCPFlagSyn) != 0 { // received SYN while in listen, proceed to form new conn
				// create the new socket
				// need to figure out what the local addr and local port would be
				socketData := SocketData{LocalAddr: 0, LocalPort: 0, DestAddr: destAddr, DestPort: destPort}

				// create a new tcb entry to represent the spawned socket connection
				newTCBEntry := &TCB{ConnectionType: 0,
					ReceiveChan: make(chan SocketData),
					SND:         &Send{Buffer: make([]byte, 0)},
					RCV:         &Receive{Buffer: make([]byte, 0)}}

				newTCBEntry.RCV.NXT = tcpHeader.SequenceNumber() + 1 // acknowledge the SYN's seq number (X+1)
				newTCBEntry.RCV.IRS = tcpHeader.SequenceNumber()     // start our stream at seq number (X)
				newTCBEntry.SND.ISS = NewISS()                       // start with random ISS for our SYN (= Y)
				newTCBEntry.SND.NXT = newTCBEntry.SND.ISS + 1        // next to send is (Y+1)
				newTCBEntry.SND.UNA = newTCBEntry.SND.ISS            // waiting for acknowledgement of (Y)
				newTCBEntry.State = SYN_RECEIVED                     // move to SYN_RECEIVED
				newTCBEntry.ListenKey = key
				t.SocketTable[socketData] = newTCBEntry

				// send SYN-ACK
				tcpHeader := CreateTCPHeader(srcPort, destPort, tcbEntry.SND.ISS, tcbEntry.RCV.NXT, header.TCPFlagSyn|header.TCPFlagAck)
				buf := MarshallTCPHeader(&tcpHeader, destAddr)

				// send to IP layer
				t.IPLayerChannel <- buf
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
				tcbEntry.RCV.IRS = tcpHeader.SequenceNumber()     // start of client's stream is (Y)
				tcbEntry.SND.UNA = tcpHeader.AckNumber()          // the X we sent in SYN is ACK'd, set unACK'd to X++1

				if tcbEntry.SND.UNA > tcbEntry.SND.ISS {
					tcbEntry.State = ESTABLISHED

					tcbEntry.SND.ISS = NewISS()

					// create the tcp header with the ____
					tcpHeader := CreateTCPHeader(srcPort, destPort, tcbEntry.SND.NXT, tcbEntry.RCV.NXT, header.TCPFlagAck)
					buf := MarshallTCPHeader(&tcpHeader, destAddr)

					tcbEntry.ReceiveChan <- key // signal the socket that is waiting for accepts to proceed with returning a new socket
					t.IPLayerChannel <- buf     // send this to the ip layer channel so it can be sent as data
					return
				}

				// (simultaneous open), ACK is 0 (just SYN)
				// enter SYN_RECEIVED
				tcbEntry.State = SYN_RECEIVED

				// need to send SYN_ACK in this case
				tcbEntry.SND.ISS = NewISS() // start with a new (Y)

				msgtcpHeader := CreateTCPHeader(srcPort, destPort, tcbEntry.SND.ISS, tcbEntry.RCV.NXT, header.TCPFlagSyn|header.TCPFlagAck)
				buf := MarshallTCPHeader(&msgtcpHeader, destAddr)

				t.IPLayerChannel <- buf

				tcbEntry.SND.Window = uint32(tcpHeader.WindowSize())
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

					tcbEntry.SND.Window = uint32(tcpHeader.WindowSize())

					// send to accept
					// TODO: temporary solution is just adding an additional field to store the listener reference
					listenerEntry := t.SocketTable[tcbEntry.ListenKey]
					listenerEntry.ReceiveChan <- key
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
