package tcp

import (
	"math/rand"

	"github.com/google/netstack/tcpip/header"
)

// tcp_handler_utils houses reusable functions for the tcp handler

func CreateTCPHeader(srcPort uint16, dstPort uint16, seqNum uint32, ackNum uint32, flags uint8, windowSize uint32) header.TCPFields {
	// log.Printf("create TCP header window size: %d\n", windowSize)
	return header.TCPFields{
		SrcPort:       srcPort,
		DstPort:       dstPort,
		SeqNum:        seqNum,
		AckNum:        ackNum,
		DataOffset:    20,
		Flags:         flags,
		WindowSize:    uint16(windowSize),
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
	return val % MAX_BUF_SIZE
}

func Min(val1 uint32, val2 uint32) uint32 {
	if val1 < val2 {
		return val1
	} else {
		return val2
	}
}

// extra interface functions

func (t *TCPHandler) AddChanRoutine() {
	return
}

func (t *TCPHandler) RemoveChanRoutine() {
	return
}
