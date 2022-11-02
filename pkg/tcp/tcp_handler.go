package tcp 

import (
	"encoding/binary"
	"ip/pkg/socket"
	"net"

	"github.com/google/netstack/tcpip/header"
)

type SocketData struct {
	LocalAddr uint32
	LocalPort uint16
	DestAddr  uint32
	DestPort  uint16
}

type TCB struct {
	State  socket.ConnectionState
	Socket *socket.Socket
	ReceiveChan chan 
}

type TCPHandler struct {
	SocketTable    map[SocketData]*TCB // maps tuple to representation of the socket
	IPLayerChannel chan []byte                           // connect with the host layer about TCP packet
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

	// create a SYN packet with a random sequence number
	newTCBEntry := &TCB{}
	CreateTCPPacket

	t.IPLayerChannel <- packet 
	// wait for SYN + ACK
	
	// wait for ACK 
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
func Listen(port uint16) (*socket.VTCPListener, error) {
	// check if port is already used
	// for 
	
	// initialize tcb with listen state, return listener socket conn
	newTCBEntry := TCB{}

	socket.VTCPListener

}

func ReceivePacket(packet IPPacket, data interface{}) {
	// need to figure out which socket that it corresponds to
	// follow the state machine for when we receive a packet

	// check the table for the connection 

	// connection could not exist 

	// if connection exists: 

	// state: 
	switch state_of_connection {
	case socket CLOSED: 
			// call individual function that will handle the sending to respective channel 
	case socket.SYN_SENT: 
	case socket.
	}
}

func InitHandler(data []interface{}) {

}

func AddChanRoutine() {
	return
}

func RemoveChanRoutine() {
	return
}
