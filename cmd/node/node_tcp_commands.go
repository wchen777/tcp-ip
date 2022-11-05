package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"tcp-ip/pkg/tcp"
)

func (n *Node) AcceptCommand(port uint16) error {
	listener, err := n.VListen(port)
	n.AddToTable(listener)
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

func (n *Node) ListSocketCommand(w io.Writer) {
	fmt.Fprintf(w, "socket\tlocal-addr\tport\tdst-addr\tport\tstatus\n")
	for i, socket := range n.SocketIndexTable {
		socketData := socket.GetSocketTableKey()
		localAddr := addrNumToIP(socketData.LocalAddr)
		destAddr := addrNumToIP(socketData.DestAddr)
		status := tcp.SocketStateToString(n.TCPHandler.SocketTable[socketData].State)
		fmt.Fprintf(w, "%d\t%s\t%d\t%s\t%d\t%s\n", i, localAddr, socketData.LocalPort, destAddr, socketData.DestPort, status)
	}
}
