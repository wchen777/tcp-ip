package main

import (
	"fmt"
	"log"
	"net"
	"tcp-ip/pkg/tcp"
)

func (n *Node) AcceptCommand(port uint16) error {
	listener, err := n.VListen(port)
	if err != nil {
		return err
	}
	go n.acceptHelper(listener)
	return nil
}

func (n *Node) AddToTable(socket tcp.Socket) int {
	for i, socket := range n.SocketIndexTable {
		if socket == nil {
			n.SocketIndexTable[i] = socket
			return i
		}
	}
	n.SocketIndexTable = append(n.SocketIndexTable, socket)
	return len(n.SocketIndexTable) - 1
}

func (n *Node) acceptHelper(listener *tcp.VTCPListener) {
	for {
		// once accept returns, create a new entry in socket index table
		newConn, err := listener.VAccept()
		if err != nil {
			log.Print(err)
			return
		}

		newSocketIndex := n.AddToTable(newConn)

		fmt.Printf("v_accept() returned %d\n", newSocketIndex)
	}
}

func (n *Node) ConnectCommand(destAddr net.IP, port uint16) (int, error) {
	newConn, err := n.TCPHandler.Connect(destAddr, port)

	if err != nil {
		return -1, err
	}

	return n.AddToTable(newConn), nil
}

// func (n *Node) ListSocketCommand() {
// 	for n.SocketIndexTable()
// }
