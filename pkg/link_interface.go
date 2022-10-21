package pkg

import (
	"fmt"
	"net"

	"golang.org/x/net/ipv4"
)

const (
	MTU = 1400
)

// This is responsible for the interface to send/receive messages on a certain UDP port
// If a host has multiple interfaces, they all still use the same UDP port to communicate

// We're assuming that each interface will be able to detect where to send messages
// based on the routing table
type LinkComm interface {
	Listen() error
	Send(ip_packet IPPacket) error
	Enable()
	Disable()
}

type LinkInterface struct {
	InterfaceNumber int
	HostIPAddress   uint32 // this is us
	DestIPAddress   uint32 // this is the addr of the neighbor connected to the interface

	HostConnection  *net.UDPConn
	DestConnection  *net.UDPConn // this connects from current interface to the immediate link connection interface
	IPPacketChannel chan IPPacket

	UDPDestPort string
	UDPDestAddr string

	Stopped bool
}

/*
	initialize connection to host on other side interface
*/
func (c *LinkInterface) InitializeDestConnection() (err error) {
	addrString := fmt.Sprintf("%s:%s", c.UDPDestAddr, c.UDPDestPort)
	udpAddr, err := net.ResolveUDPAddr("udp4", addrString)

	UDPConn, err := net.DialUDP("udp4", nil, udpAddr)
	if err != nil {
		return
	}
	c.DestConnection = UDPConn

	return
}

/*
	Receive an ip packet from the link layer and send it to the ip layer
*/
func (c *LinkInterface) Listen() (err error) {

	for {
		// TODO: pls fix this. THIS IS BAD!!!!!! BAD FOR OUR HEALTH PLUS CPU HEALTH
		// Take a look at sync.Cond, sync.WaitGroup
		if c.Stopped {
			continue
		}

		// always listening for packets
		buffer := make([]byte, MTU)
		numBytes, _, err := c.HostConnection.ReadFromUDP(buffer)
		if err != nil {
			fmt.Print(err)
			return err
		}

		fmt.Printf("Number of bytes read from link layer connection: %d\n", numBytes)

		// deserialize into IPPacket to return
		ipPacket := IPPacket{}
		hdr, _ := ipv4.ParseHeader(buffer)
		ipPacket.Header = *hdr
		ipPacket.Data = buffer[hdr.Len:]

		// send to network layer from link layer
		c.IPPacketChannel <- ipPacket
	}
}

/*
	send an ip packet through the link layer to a destination interface
*/
func (c *LinkInterface) Send(ip_packet IPPacket) {
	if c.Stopped {
		return
	}

	// serializng IP packet into array of bytes
	bytes, _ := ip_packet.Header.Marshal()
	bytes = append(bytes, ip_packet.Data...)

	c.DestConnection.Write(bytes)
	return
}

/*
	enable/disable link interface at runtime
*/
func (c *LinkInterface) Enable() {
	c.Stopped = false
}

/*
	enable/disable link interface at runtime
*/
func (c *LinkInterface) Disable() {
	c.Stopped = true
}
