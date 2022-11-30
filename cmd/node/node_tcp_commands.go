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
	"time"
)

// Open a socket, bind it to the given port on any interface, and start accepting connections on that port.
func (n *Node) AcceptCommand(port uint16) error {
	listener, err := n.VListen(port)
	n.AddToTable(listener)
	if err != nil {
		return err
	}
	go n.acceptHelper(listener)
	return nil
}

// helper function for accept to add a returned socket to the socket index table
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

// listens for an individual connection
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

// Attempt to connect to the given IP address, in dot notation, on the given port. Example: c 10.13.15.24 1056
func (n *Node) ConnectCommand(destAddr net.IP, port uint16) (int, error) {
	newConn, err := n.TCPHandler.Connect(destAddr, port)

	if err != nil {
		return -1, err
	}

	return n.AddToTable(newConn), nil
}

func (n *Node) ListSocketCommand(w io.Writer) {
	toDelete := make([]int, 0)
	fmt.Fprintf(w, "socket\tlocal-addr\tport\tdst-addr\tport\tstatus\tccontrol\tcwnd\n")
	for i, socket := range n.SocketIndexTable {
		if socket == nil {
			continue
		}
		socketData := socket.GetSocketTableKey()
		if n.TCPHandler.SocketTable[socketData] == nil {
			toDelete = append(toDelete, i)
			continue
		}
		localAddr := addrNumToIP(socketData.LocalAddr)
		destAddr := addrNumToIP(socketData.DestAddr)
		status := tcp.SocketStateToString(n.TCPHandler.SocketTable[socketData].State)

		// Need to print out information for congestion control
		var congestionControl string
		var congestionWindow uint32
		if n.TCPHandler.SocketTable[socketData].CControlEnabled.Load() {
			congestionControl = "tahoe"
			congestionWindow = n.TCPHandler.SocketTable[socketData].CWND
		} else {
			congestionControl = "none"
			congestionWindow = 0
		}
		fmt.Fprintf(w, "%d\t%s\t%d\t%s\t%d\t%s\t%s\t%d\n", i, localAddr, socketData.LocalPort, destAddr,
			socketData.DestPort, status, congestionControl, congestionWindow)
	}

	for _, index := range toDelete {
		n.SocketIndexTable[index] = nil
	}
}

// Send a string on a socket. This should block.
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

// Try to read data from a given socket.
// If the last argument is y, then you should block until numbytes is received, or the connection closes.
// If n, then do not block and read whatever is in the socket
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
	if err != nil {
		return err
	}
	fmt.Printf("Number of bytes read: %d\n", numBytesRead)
	fmt.Printf("Data read: %s", string(data))
	return err
}

func (n *Node) HandleDeletion(socketID int, deletionChan chan bool) {
	for {
		select {
		case <-deletionChan:
			// Only when close returns should we remove it from the socket table
			n.SocketIndexTable[socketID] = nil
		}
	}
}

// noRead is true --> cannot write to socket during close
// noWrite is false --> can still write to socket during close
func (n *Node) CloseTCPCommand(socketID int, noRead bool) error {
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
			listenerVal.VClose()
			return nil
		} else {
			return errors.New("Socket doesn't have close function")
		}
	} else {
		conn = val
	}

	// set flags to block application level operations
	if noRead {
		conn.ReadCancelled.Store(true)

	}
	conn.WriteCancelled.Store(true)

	deletionChan := make(chan bool)
	go func() {
		// a goroutine that notifies another goroutine that close has returned
		if noRead { // if no read is specified, we are closing both ends for this function
			conn.VClose()
		} else { // otherwise, close just write (still able to read)
			conn.VShutdown(1)
		}
		deletionChan <- true
	}()
	go n.HandleDeletion(socketID, deletionChan)
	fmt.Printf("Socket %d has been closed\n", socketID)
	return nil
}

// VShutdown on the given socket. If read or r is given, close only the reading side.
// If write or w is given, close only the writing side.
// If both is given, close both sides. Default is write.
func (n *Node) ShutDownTCPCommand(socketID int, option string) error {
	if socketID < 0 || socketID >= len(n.SocketIndexTable) || n.SocketIndexTable[socketID] == nil {
		return errors.New("Invalid socket ID")
	}

	socketToShutdown := n.SocketIndexTable[socketID]
	// maybe we can set some field in here to say that the read operation is invalid
	if val, ok := socketToShutdown.(*tcp.VTCPConn); !ok {
		return errors.New("Cannot shutdown read on listening conn")
	} else {
		if option == "read" || option == "r" {
			// this just shuts down reading, but we can still write
			// the socket remains in ESTABLISHED state
			val.VShutdown(0)
		} else if option == "write" || option == "w" {
			// It looks like just a normal CLOSE as defined in the RFC, except we can still read
			n.CloseTCPCommand(socketID, false) // close just write (still able to read)
		} else if option == "both" {
			n.CloseTCPCommand(socketID, true) // close both read and write
		} else {
			return errors.New("Invalid option for shutdown")
		}
	}
	return nil
}

// Connect to the given ip and port, send the entirety of the specified file, and close the connection.
func (n *Node) SendFileTCPCommand(filepath string, ipAddr string, port uint16, ccontrol bool) error {
	addr := net.ParseIP(ipAddr)
	if addr == nil {
		return errors.New("Invalid ip address\n")
	}

	f, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer f.Close()

	// create a new connection
	newConn, err := n.TCPHandler.Connect(addr, port)
	log.Print("returning from connect")
	if err != nil {
		return err
	}
	// and add it to the socket table
	socketID := n.AddToTable(newConn)

	if ccontrol {
		// enable congestion control for the socket
		n.TCPHandler.SetCCForSocket(newConn)
	}

	// read file until EOF and send each amount that we read
	// perhaps we can read like a chunk size each time? aka 1024 bytes
	// TODO: is it okay to read chunk size?
	bytesWritten := uint32(0)
	for {
		buf := make([]byte, 1024)
		amountRead, err := f.Read(buf)
		if amountRead == 0 {
			break
		}
		if err != nil {
			return err
		}
		numBytesWritten, err := newConn.VWrite(buf[:amountRead])
		if err != nil {
			return err
		}
		bytesWritten += numBytesWritten
		//log.Printf("bytes written so far: %d\r\n", bytesWritten)
	}
	fmt.Printf("Total bytes written: %d\n", bytesWritten)

	// close the connection here
	newConn.VClose()
	n.SocketIndexTable[socketID] = nil

	return nil
}

// Listen for a connection on the given port.
// Once established, write everything you can read from the socket to the given file.
// Once the other side closes the connection, close the connection as well.
func (n *Node) ReadFileTCPCommand(filepath string, port uint16) error {
	// start listening on port number that is specified
	listener, err := n.VListen(port)
	if err != nil {
		return err
	}

	n.AddToTable(listener)

	// a new connection has been established, so we should
	newConn, err := listener.VAccept()

	// adding the new connection to the table
	n.AddToTable(newConn)

	// create a buffer to read data into
	buf := make([]byte, 4096)

	// open the file to write what is read to the file
	f, err := os.Create(filepath)

	go func() {
		bytesReadTotal := uint32(0)
		for {
			numbytes, err := newConn.VRead(buf, 4096, false)
			if err != nil {
				log.Print(err.Error())
				newConn.VClose()
				// close the listener connection as well
				listener.VClose()
				f.Close()
				fmt.Printf("Total bytes read: %d\n", bytesReadTotal)
				return
			}
			bytesReadTotal += numbytes

			_, err = f.Write(buf[:numbytes])
			if err != nil {
				log.Print(err)
				return
			}

			//log.Printf("bytes read so far: %d\r\n", bytesReadTotal)
			time.Sleep(10 * time.Millisecond)
		}
	}()

	return nil
}
