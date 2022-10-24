package traceroute

import "golang.org/x/net/ipv4"

// general approach:
// send a packet with TTL 1 --> when does TTL get decremented?
// define some protocol number

type TimeExceededMessage struct {
	Type     uint8
	Code     uint8
	Checksum uint16
	Unused   uint32 // TODO: what's the best way to represent this
	IPHeader ipv4.Header
	Data     []byte // first 8 bytes of data
}
