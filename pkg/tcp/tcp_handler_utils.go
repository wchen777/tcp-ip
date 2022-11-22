package tcp

import (
	"encoding/binary"
	"log"
	"math/rand"

	"github.com/google/netstack/tcpip/header"
)

const (
	TCPProtocolNum     = header.TCPProtocolNumber
	TCPPseudoHeaderLen = 12
	TCPHeaderLen       = header.TCPMinimumSize
)

// tcp_handler_utils houses reusable functions for the tcp handler

func ComputeTCPChecksum(tcpHdr *header.TCPFields,
	sourceIP uint32, destIP uint32, payload []byte) uint16 {

	pshBytes := make([]byte, 12)

	binary.BigEndian.PutUint32(pshBytes[0:4], sourceIP)
	binary.BigEndian.PutUint32(pshBytes[4:8], destIP)

	pshBytes[8] = uint8(0)
	pshBytes[9] = uint8(TCPProtocolNum)

	totalLen := TCPHeaderLen + len(payload)
	binary.BigEndian.PutUint16(pshBytes[10:12], uint16(totalLen))

	headerBytes := header.TCP(make([]byte, TCPHeaderLen))
	headerBytes.Encode(tcpHdr)

	pseudoHeaderChecksum := header.Checksum(pshBytes, 0)
	headerChecksum := header.Checksum(headerBytes, pseudoHeaderChecksum)
	fullChecksum := header.Checksum(payload, headerChecksum)

	return fullChecksum ^ 0xffff
}

// return a header's TCP fields struct from a header.TCP object
func ParseTCPHeader(tcpHeader *header.TCP) header.TCPFields {
	return header.TCPFields{
		SrcPort:    tcpHeader.SourcePort(),
		DstPort:    tcpHeader.DestinationPort(),
		SeqNum:     tcpHeader.SequenceNumber(),
		AckNum:     tcpHeader.AckNumber(),
		DataOffset: tcpHeader.DataOffset(),
		Flags:      tcpHeader.Flags(),
		WindowSize: tcpHeader.WindowSize(),
		Checksum:   tcpHeader.Checksum(),
	}
}

func CreateTCPHeader(srcAddr uint32, destAddr uint32, srcPort uint16, dstPort uint16, seqNum uint32, ackNum uint32, flags uint8, windowSize uint32, payload []byte) header.TCPFields {
	// log.Printf("create TCP header window size: %d\n", windowSize)
	tcpHdr := header.TCPFields{
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
	checkSum := ComputeTCPChecksum(&tcpHdr, srcAddr, destAddr, payload)
	log.Printf("computed checksum: %d\n", checkSum)
	tcpHdr.Checksum = checkSum
	return tcpHdr
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

func Min64f(val1 float64, val2 float64) float64 {
	if val1 < val2 {
		return val1
	} else {
		return val2
	}
}

func Max64f(val1 float64, val2 float64) float64 {
	if val1 > val2 {
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
