package pkg

import "net"

type LinkComm interface {
	Listen() (IPPacket, error)
	Send(ip_packet IPPacket) error
}

type HostLinkComm struct {
	UDPport        uint16
	HostConnection *net.UDPConn
}

func (c *HostLinkComm) Listen() (IPPacket, error) {
	// TODO: serialize the IP Packet into a sequence of bytes
	for {
		// always listening for connections?
		c.HostConnection.Write()
	}
}

func (c *HostLinkComm) Send(ip_packet IPPacket) {
	c.HostConnection.Write()
}
