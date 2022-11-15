package tcp

import (
	"encoding/binary"
	"log"
	"sync"
	"tcp-ip/pkg/ip"

	"github.com/google/netstack/tcpip/header"
)

// The TCP Handler file contains the function implementations for the application handler interface
// and struct definitions used in the application layer for TCP protocol.
// See tcp_handler_core for the functions that implement the "network stack"

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
	// MAX_BUF_SIZE = (1 << 16) - 1 // buffer size
	MAX_BUF_SIZE = 10
	MSS_DATA     = 1360 // maximum segment size of the data in the packet, 1400 - TCP_HEADER_SIZE - IP_HEADER_SIZE
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
	SendBlockedCond  sync.Cond
	ZeroBlockedCond  sync.Cond
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
	IPErrorChannel  chan error
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
			log.Printf("received a segment when state is in LISTEN")
			t.HandleStateListen(tcpHeader, localAddr, srcPort, destAddr, destPort, &key)
		case SYN_SENT: // looking to get a SYN ACK to acknowledge our SYN and try and SYN with us
			log.Printf("received a segment when state is in SYN_SENT")
			t.HandleStateSynSent(tcpHeader, tcbEntry, localAddr, srcPort, destAddr, destPort, &key)
		case SYN_RECEIVED:
			log.Printf("received a segment when state is in SYN_RECEIVED")
			t.HandleStateSynReceived(tcpHeader, tcbEntry, &key)
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
