package traceroute

import "golang.org/x/net/ipv4"

// general approach:
// send a packet with TTL 1 --> when does TTL get decremented?
// define some protocol number

type ICMPHeader struct {
	Type        uint8
	Code        uint8
	Checksum    uint16
	Identifier  uint16
	SequenceNum uint16
}

type TimeExceededMessage struct {
	Header   ICMPHeader
	IPHeader ipv4.Header // header of the original packet
	Data     []byte      // first 8 bytes of data
}

type Echo struct {
	Header ICMPHeader
	Data   []byte
}
