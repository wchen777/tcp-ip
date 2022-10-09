package pkg

import (
	"fmt"
	"net"
)

const (
	MTU = 1400
)

type LinkComm interface {
	Listen() (IPPacket, error)
	Send(ip_packet IPPacket) error
}

type HostLinkComm struct {
	UDPport        uint16
	HostConnection *net.UDPConn
}

func (c *HostLinkComm) Listen() (IPPacket, error) {
	for {
		// always listening for connections?
		buffer := make([]byte, MTU)
		numBytes, _, _ := c.HostConnection.ReadFromUDP(buffer)

		fmt.Printf("Number of bytes read from link layer connection: %d\n", numBytes)

		// TODO: deserialize
	}
}

func (c *HostLinkComm) Send(ip_packet IPPacket) error {
	// TODO: add the header for the link layer

	// TODO: serialize the link layer + ip packet
	c.HostConnection.Write()
}
