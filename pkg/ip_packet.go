package pkg

import (
	"golang.org/x/net/ipv4"
)

type IPPacket struct {
	Header ipv4.Header
	Data   []byte
}
