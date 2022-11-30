package main

import (
	"errors"
	"fmt"
	"tcp-ip/pkg/tcp"
)

func (n *Node) ListCongestionControl() {
	fmt.Print("Congestion control algorithms available: \n")
	fmt.Print("--tahoe\n")
}

func (n *Node) SetCongestionControl(socketId int, congestionControl bool) error {
	if socketId < 0 || socketId >= len(n.SocketIndexTable) || n.SocketIndexTable[socketId] == nil {
		return errors.New("Invalid socket id\n")
	}

	socket := n.SocketIndexTable[socketId]
	var conn *tcp.VTCPConn
	if val, ok := socket.(*tcp.VTCPConn); !ok {
		return errors.New("Cannot use send command on socket\n")
	} else {
		conn = val
	}
	n.TCPHandler.SetCCForSocket(conn)
	return nil
}
