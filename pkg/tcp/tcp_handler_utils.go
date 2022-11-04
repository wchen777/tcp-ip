package tcp

import (
	"bytes"
	"encoding/binary"
	"github.com/google/netstack/tcpip/header"
	"math/rand"
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

	bytesArray := &bytes.Buffer{}
	_ = binary.Write(bytesArray, binary.BigEndian, destAddr) // assume this does not fail
	buf := bytesArray.Bytes()
	buf = append(buf, tcpBytes...)
	return buf
}

func NewISS() uint32 {
	return rand.Uint32()
}

// extra interface functions

func (t *TCPHandler) AddChanRoutine() {
	return
}

func (t *TCPHandler) RemoveChanRoutine() {
	return
}
