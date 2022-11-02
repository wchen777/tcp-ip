package ip

import (
	"fmt"
	"net"
	"sync"
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
	IPPacketChannel chan IPPacket

	UDPDestPort int
	UDPDestAddr string

	UDPAddr *net.UDPAddr

	Stopped     bool
	StoppedLock sync.Mutex
}

/*
	initialize connection to host on other side interface
*/
func (c *LinkInterface) InitializeDestConnection(addr, port string) (err error) {
	addrString := fmt.Sprintf("%s:%s", addr, port)
	udpAddr, err := net.ResolveUDPAddr("udp4", addrString)

	if err != nil {
		return
	}

	c.UDPAddr = udpAddr

	return
}

/*
	send an ip packet through the link layer to a destination interface
*/
func (c *LinkInterface) Send(ip_packet IPPacket) {
	c.StoppedLock.Lock()
	if c.Stopped {
		c.StoppedLock.Unlock()
		// log.Printf("blocked sending for this addr: %d\n", c.HostIPAddress)
		return
	}
	c.StoppedLock.Unlock()

	// serializing IP packet into array of bytes
	bytes, _ := ip_packet.Header.Marshal()
	bytes = append(bytes, ip_packet.Data...)

	_, _ = c.HostConnection.WriteToUDP(bytes, c.UDPAddr)

	return
}

/*
	enable/disable link interface at runtime
*/
func (c *LinkInterface) Enable() {
	c.StoppedLock.Lock()
	defer c.StoppedLock.Unlock()
	c.Stopped = false
}

/*
	enable/disable link interface at runtime
*/
func (c *LinkInterface) Disable() {
	c.StoppedLock.Lock()
	defer c.StoppedLock.Unlock()
	c.Stopped = true
}
