package tcp

import (
	"encoding/binary"
	"ip/pkg/socket"
	"net"
	"tcp-ip/pkg/ip"

	"github.com/google/netstack/tcpip/header"
)

// TODO: do we need to include the protocol as part of this tuple?
type SocketData struct {
	LocalAddr uint32
	LocalPort uint16
	DestAddr  uint32
	DestPort  uint16
}

type TCB struct {
	State       socket.ConnectionState
	Socket      *socket.Socket
	ReceiveChan chan []byte // some sort of channel when receiving another message on this layer
	LocalAddr   uint32
	// need to store send and receive buffers in the TCB
	// TODO: should these be a part of the socket itself or in the TCB?
	SendBuffer    []byte
	ReceiveBuffer []byte
}

type TCPHandler struct {
	SocketTable    map[SocketData]*TCB // maps tuple to representation of the socket
	IPLayerChannel chan []byte         // connect with the host layer about TCP packet
}

func CreateTCPPacket(srcPort uint16, dstPort uint16, seqNum uint32, ackNum uint32) header.TCPFields {
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
func (t *TCPHandler) Connect(addr net.IP, port uint16) (*tcp.VTCPConn, error) {
	destAddr := binary.BigEndian.Uint32(addr.To4())

	// TODO: first need to check if the entry exists? Yes, and return error in this case

	// create a SYN packet with a random sequence number
	// create a TCB entry and socketData entry 
	newTCBEntry := &TCB{socket.SYN_SENT, }
	socketData := SocketData{LocalAddr: , LocalPort: , DestAddr: , DestPort: }
	
	// create TCP packet to send a SYN 
	// TODO: should the sending all be done with the send function? 
	// and have the send function switch on the different states?
	// or should each function handle it's sending separately? 
	t.IPLayerChannel <- packet
	// wait for SYN + ACK from server 

	// sends ACK to server 

	// go into ESTABLISHED state and return the new VTCPConn object 
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
func (t *TCPHandler) Listen(port uint16) (*socket.VTCPListener, error) {
	// TODO: check if port is already used

	// initialize tcb with listen state, return listener socket conn
	// go into the LISTEN state 
	socketData := SocketData{LocalAddr: , LocalPort: , DestAddr: , DestPort: }

	// if the entry doesn't yet exist 
	newTCBEntry := &TCB{
		State: ,
		Socket: ,
		ReceiveChan: ,
		LocalAddr: ,
		SendBuffer: ,
		ReceiveBuffer: 
	}

	socket.VTCPListener
}

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
		// call appropriate function for each state
		// TODO: is it necessary that we switch here or can we switch 
				 // after a client receives a packet in a channel? 
		switch tcbEntry.State {
		case socket.CLOSED:
			// TODO: what happens if we send something in the channel but the
			// socket isn't actually expecting that message? how do we prevent blocking? 
		case socket.LISTEN:
		case socket.SYN_SENT:
		case socket.SYN_RECEIVED:
		case socket.ESTABLISHED:
		case socket.FIN_WAIT_1:
		case socket.FIN_WAIT_2:
		case socket.CLOSING:
		case socket.TIME_WAIT:
		case socket.CLOSE_WAIT:
		case socket.LAST_ACK:
		}
	} else {
		// connection does not exist
		// TODO: what to do here
	}
}

// eventually each of the "socket" objects will make a call to the TCP handler stack 
// can pass in the socket object that is making the call
func (t *TCPHandler) Accept(vl *VTCPListener) (*VTCPConn, error) {
	// this will block until a SYN is received from a client that's trying to connect
	
	// send SYN ACK 

	// wait for ACK to be received 

	// create entry + return once connection is established 
} 

// corresponds to RECEIVE in RFC 
func (t *TCPHandler) Read(data []byte, vc *VTCPConn) (int, error) {
	// maybe we can check the 
}

// corresponds to SEND in RFC 
func (t *TCPHandler) Write(data []byte, vc *VTCPConn) (int, error) {
	// check to see if the connection exists in the table, return error if it doesn't exist 

	// Per section 3.9, we can check the state of the connection and handle according to the state

}

func (t *TCPHandler) Shutdown(sdType int, vc *VTCPConn) error {

}

// Both socket types will need a close, so pass in socket instead 
// then we can call getTye to get the exact object 
func (t *TCPHandler) Close(socket *Socket) error {

}


func (t *TCPHandler) InitHandler(data []interface{}) {

}

func (t *TCPHandler) AddChanRoutine() {
	return
}

func (t *TCPHandler) RemoveChanRoutine() {
	return
}
