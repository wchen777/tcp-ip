package pkg

import (
	"golang.org/x/net/ipv4"
)

type IPPacket struct {
	header ipv4.Header
	data   []byte
}
