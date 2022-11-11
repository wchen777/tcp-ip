package tcp

import (
	"math/rand"

	"github.com/google/netstack/tcpip/header"
)

// tcp_handler_utils houses reusable functions for the tcp handler

func CreateTCPHeader(srcPort uint16, dstPort uint16, seqNum uint32, ackNum uint32, flags uint8) header.TCPFields {
	return header.TCPFields{
		SrcPort:       srcPort,
		DstPort:       dstPort,
		SeqNum:        seqNum,
		AckNum:        ackNum,
		DataOffset:    20,
		Flags:         flags,
		WindowSize:    65535,
		Checksum:      0,
		UrgentPointer: 0,
	}
}

func MarshallTCPHeader(tcpHeader *header.TCPFields, destAddr uint32) []byte {
	tcpBytes := make(header.TCP, header.TCPMinimumSize)
	tcpBytes.Encode(tcpHeader)

	return tcpBytes
}

func NewISS() uint32 {
	return rand.Uint32()
}

func SequenceToBufferInd(val uint32) uint32 {
	return val % (1 << 16)
}

// extra interface functions

func (t *TCPHandler) AddChanRoutine() {
	return
}

func (t *TCPHandler) RemoveChanRoutine() {
	return
}
