package pkg

import (
	"bytes"
	"encoding/binary"
	"ip/pkg/traceroute"
)

const (
	ECHO          = 8
	ECHO_REPLY    = 0
	TIME_EXCEEDED = 11
)

type NextHopMsg struct {
	Found   bool
	NextHop uint32
}

type TracerouteHandler struct {
	RouteChan chan NextHopMsg
	EchoChan  chan []byte // sending replies back to the src
}

// routine for when we receive something from the host
func (tr *TracerouteHandler) ReceivePacket(packet IPPacket, data interface{}) {
	// what we do when we receive an ICMP message
	// deserialize the packet data
	var typeMsg uint8

	// create a buffer for the first byte of data
	packetDataBuf := bytes.NewReader(packet.Data[0:1])
	binary.Read(packetDataBuf, binary.BigEndian, &typeMsg)

	switch typeMsg {
	case ECHO:
		// we have reached our destination and we should ask the host to
		// send a message back to the host to send an echo reply back to the src in the echo message
		dataBytes := make([]byte, 64)
		packetDataBuf = bytes.NewReader(packet.Data[8:])
		binary.Read(packetDataBuf, nil, dataBytes)

		header := traceroute.ICMPHeader{
			Type:        uint8(8),
			Code:        uint8(0),
			Checksum:    uint16(0),
			Identifier:  uint16(0),
			SequenceNum: uint16(0)}
		echoMessage := traceroute.Echo{
			Header: header,
			Data:   dataBytes}

		bytesArray := &bytes.Buffer{}
		binary.Write(bytesArray, binary.BigEndian, binary.BigEndian.Uint32(packet.Header.Src.To4()))
		binary.Write(bytesArray, binary.BigEndian, echoMessage)
		tr.EchoChan <- bytesArray.Bytes()
	case ECHO_REPLY:
		// the destination we are looking for is in fact reachable
		// and is in the src of the message
		// send the next hop in the sequence aka the
		tr.RouteChan <- NextHopMsg{Found: true, NextHop: binary.BigEndian.Uint32(packet.Header.Src.To4())}
	case TIME_EXCEEDED:
		// TODO: how to handle if there's no one reading from this channel or
		// no traceroute command being executed
		tr.RouteChan <- NextHopMsg{Found: true, NextHop: binary.BigEndian.Uint32(packet.Header.Src.To4())}
	}
}

func (tr *TracerouteHandler) InitHandler(data []interface{}) {
	return
}
