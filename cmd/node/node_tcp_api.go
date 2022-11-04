package main

import (
	"net"
	"tcp-ip/pkg/tcp"
)

func (n *Node) VConnect(addr net.IP, port uint16) (*tcp.VTCPConn, error) {
	return n.TCPHandler.Connect(addr, port)
}

func (n *Node) VListen(port uint16) (*tcp.VTCPListener, error) {
	return n.TCPHandler.Listen(port)
}
