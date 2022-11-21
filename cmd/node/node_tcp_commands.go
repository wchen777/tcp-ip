package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
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

		if newConn == nil {
			log.Print("Listener connection terminated, dropping new pending connection")
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
		if socket == nil {
			continue
		}
		socketData := socket.GetSocketTableKey()
		localAddr := addrNumToIP(socketData.LocalAddr)
		destAddr := addrNumToIP(socketData.DestAddr)
		status := tcp.SocketStateToString(n.TCPHandler.SocketTable[socketData].State)
		fmt.Fprintf(w, "%d\t%s\t%d\t%s\t%d\t%s\n", i, localAddr, socketData.LocalPort, destAddr, socketData.DestPort, status)
	}
}

func (n *Node) SendTCPCommand(line string) error {

	args := strings.SplitN(line, " ", 3)

	socketID, err := strconv.Atoi(args[1])
	if err != nil {
		fmt.Printf("Invalid socket id number: %s\n", args[1])
	}

	if socketID >= len(n.SocketIndexTable) {
		return errors.New("Invalid socket id\n")
	}

	socketToSend := n.SocketIndexTable[socketID]

	var connSend *tcp.VTCPConn
	if val, ok := socketToSend.(*tcp.VTCPConn); !ok {
		return errors.New("Cannot use send command on socket\n")
	} else {
		connSend = val
	}

	bytesSent, err := connSend.VWrite([]byte(args[2]))
	log.Printf("Number of bytes sent: %d\n", bytesSent)

	return err
}

func (n *Node) ReadTCPCommand(socketID int, numBytes uint32, readAll bool) error {
	if socketID >= len(n.SocketIndexTable) {
		return errors.New("Invalid socket id\n")
	}
	socketToRead := n.SocketIndexTable[socketID]

	if socketToRead == nil {
		return errors.New("Socket does not exist\n")
	}

	var connRead *tcp.VTCPConn
	if val, ok := socketToRead.(*tcp.VTCPConn); !ok {
		return errors.New("Cannot use read command on socket\n")
	} else {
		connRead = val
	}

	data := make([]byte, numBytes)
	numBytesRead, err := connRead.VRead(data, numBytes, readAll)
	log.Printf("Number of bytes read: %d\n", numBytesRead)
	log.Printf("Data read: %s", string(data))
	return err
}

func (n *Node) CloseTCPCommand(socketID int) error {
	if socketID >= len(n.SocketIndexTable) || socketID < 0 {
		return errors.New("Invalid socket id\n")
	}
	socketToClose := n.SocketIndexTable[socketID]

	if socketToClose == nil {
		return errors.New("Socket does not exist\n")
	}

	var conn *tcp.VTCPConn
	if val, ok := socketToClose.(*tcp.VTCPConn); !ok {

		if listenerVal, ok := socketToClose.(*tcp.VTCPListener); ok {
			n.SocketIndexTable[socketID] = nil
			go listenerVal.VClose()
			return nil
		} else {
			return errors.New("Socket doesn't have close function")
		}
	} else {
		conn = val
	}

	// TODO: where should we set to be nil?
	n.SocketIndexTable[socketID] = nil
	go conn.VClose()
	fmt.Printf("Socket %d has been closed\n", socketID)
	return nil
}

func (n *Node) ShutDownTCPCommand(socketID int, option string) error {
	if option == "read" || option == "r" {
		// this just shuts down reading, but we can still write
		// the socket remains in ESTABLISHED state
	} else if option == "write" || option == "w" {
		// It looks like just a normal CLOSE as defined in the RFC, except we can still read
	} else if option == "both" {
		n.CloseTCPCommand(socketID)
	} else {
		return errors.New("Invalid option")
	}
	return nil
}

func (n *Node) SendFileTCPCommand(filepath string, ipAddr string, port uint16) error {
	addr := net.ParseIP(ipAddr)
	if addr == nil {
		return errors.New("Invalid ip address\n")
	}

	newConn, err := n.TCPHandler.Connect(addr, port)
	if err != nil {
		return err
	}
	n.AddToTable(newConn)

	if err != nil {
		return err
	}

	f, err := os.Open(filepath)
	defer f.Close()

	if err != nil {
		return err
	}

	// read file until EOF and send each amount that we read
	// perhaps we can read like a chunk size each time? aka 1024 bytes
	buf := make([]byte, 1024)
	for {
		amountRead, err := f.Read(buf)
		if amountRead == 0 {
			break
		}
		if err != nil {
			return err
		}
		_, err = newConn.VWrite(buf)
		if err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) ReadFileTCPCommand() error {
	return nil
}
