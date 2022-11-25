package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"tcp-ip/pkg/ip"
	"tcp-ip/pkg/tcp"
	"text/tabwriter"
)

const (
	RIP_PROTOCOL  = 200
	TEST_PROTOCOL = 0
	ICMP_PROTOCOL = 1
	TCP_PROTOCOL  = 6
)

// the node represents a machine that implements a host using IP routing and a TCP socket API built on top of the host
type Node struct {
	Host             *ip.Host // the node's IP host
	TCPHandler       *tcp.TCPHandler
	SocketIndexTable []tcp.Socket // essentially functions as a file descriptor table
}

/*
	node startup function
*/
func (n *Node) StartNode(filepath string) {
	// initialize all necessary data for the node from the .lnx file
	n.InitNodeFromLNX(filepath)

	// start listening for the host
	n.Host.StartHost()

	n.REPL()
}

/*
	print out help usage for the node
*/
func (n *Node) PrintHelp(w io.Writer) {
	fmt.Fprintf(w, "---------------------- IP COMMANDS --------------------\n")
	fmt.Fprintf(w, "send <ip> <proto> <string> \t - Sends the string payload to the given ip address with the specified protocol.\n")
	fmt.Fprintf(w, "down <interface-num> \t - Bring an interface \"down\".\n")
	fmt.Fprintf(w, "up <interface-num> \t - Bring an interface \"up\".\n")
	fmt.Fprintf(w, "interfaces, li <file> \t - Print information about each interface, one per line. Optionally specify a destination file.\n")
	fmt.Fprintf(w, "routes, lr <file>\t - Print information about the route to each known destination, one per line. Optionally specify a destination file.\n")
	fmt.Fprintf(w, "---------------------- TCP COMMANDS --------------------\n")
	fmt.Fprintf(w, "sockets, ls \t - List all sockets, along with the state the TCP connection associated with them is in, and their window sizes.\n")
	fmt.Fprintf(w, "a <port> \t - Open a socket, bind it to the given port on any interface, and start accepting connections on that port.\n")
	fmt.Fprintf(w, "c <ip> <port> \t - Attempt to connect to the given IP address, in dot notation, on the given port.\n")
	fmt.Fprintf(w, "s <socket ID> <data> \t - Send a string on a socket.\n")
	fmt.Fprintf(w, "r <socket ID> <numbytes> <y|n> \t - Try to read data from a given socket. y -- block until numbytes is received, n -- return whatever can be read.\n")
	fmt.Fprintf(w, "sd <socket ID> <read|write|both> \t - Shutdown the given socket. If read is given, close only the reading side. If write is given, close only the writing side.\n")
	fmt.Fprintf(w, "cl <socket ID> \t - Close the given socket.\n")
	fmt.Fprintf(w, "sf <filename> <ip> <port> \t - Connect to the given ip and port, send the entirety of the specified file, and close the connection.\n")
	fmt.Fprintf(w, "rf <filename> <port> \t - Listen for a connection on the given port. Once established, write everything you can read from the socket to the given file. Once the other side closes the connection, close the connection as well.\n")
	fmt.Fprintf(w, "----------------------------------------------------------\n")
	fmt.Fprintf(w, "quit, q \t - Quit this node cleanly.\n")
	fmt.Fprintf(w, "help, h \t - Show this help.\n")

}

/*
	q command to quit out of REPL and clean up UDP sending connections
*/
func (n *Node) Quit() {
	n.Host.CancelHost()
	n.Host.HostConnection.Close()
	os.Exit(0)
}

func (n *Node) REPL() {
	// start CLI
	scanner := bufio.NewScanner(os.Stdin)

	// for table input
	w := new(tabwriter.Writer)
	// minwidth, tabwidth, padding, padchar, flags
	w.Init(os.Stdout, 24, 10, 4, '\t', 0)

	fmt.Print("> ")
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			fmt.Print("> ")
			continue
		}
		commands := strings.Split(line, " ")

		switch commands[0] {
		case "interfaces":
			// information about interfaces
			n.PrintInterfaces(w)
			w.Flush()
			break
		case "li":
			// information about interfaces
			n.PrintInterfaces(w)
			w.Flush()
			break
		case "routes":
			// routing table information
			n.PrintRoutingTable(w)
			w.Flush()
			break
		case "lr":
			// routing table information
			n.PrintRoutingTable(w)
			w.Flush()
			break
		case "down":
			if len(commands) < 2 {
				fmt.Print("Invalid number of arguments for down")
				break
			}
			ifNum, err := strconv.Atoi(commands[1])
			if err != nil {
				fmt.Print("Invalid input for interface number")
				break
			}
			err = n.Host.DownInterface(ifNum)

			if err != nil {
				log.Print(err.Error())
			}
		case "up":
			if len(commands) < 2 {
				fmt.Print("Invalid number of arguments for up")
				break
			}
			ifNum, err := strconv.Atoi(commands[1])
			if err != nil {
				fmt.Print("Invalid input for interface number")
				break
			}

			err = n.Host.UpInterface(ifNum)

			if err != nil {
				log.Print(err.Error())
			}

		case "send":
			if len(commands) < 4 {
				fmt.Print("Invalid number of arguments for send")
				break
			}
			n.SendCommand(line)
			break
		case "q":
			if len(commands) != 1 {
				fmt.Print("Invalid number of arguments for q")
				break
			}
			n.Quit()
		case "traceroute":
			if len(commands) != 2 {
				fmt.Print("Invalid number of arguments for traceroute")
				break
			}
			n.Traceroute(commands[1])
		case "a":
			if len(commands) != 2 {
				fmt.Print("Invalid number of arguments for <a>ccept")
			}
			port, err := strconv.Atoi(commands[1])
			if err != nil {
				fmt.Printf("Invalid port number: %s\n", commands[1])
			}
			err = n.AcceptCommand(uint16(port))
			if err != nil {
				fmt.Printf("Accept error: %s\n", err)
			}
		case "c":
			if len(commands) != 3 {
				fmt.Print("Invalid number of arguments for <c>onnect")
				break
			}
			ipAddr := net.ParseIP(commands[1])
			port, err := strconv.Atoi(commands[2])
			if err != nil {
				fmt.Printf("Invalid port number: %s\n", commands[2])
				break
			}
			socketNum, err := n.ConnectCommand(ipAddr, uint16(port))
			if err != nil {
				fmt.Printf("Connect error: %s\n", err)
				break
			}
			fmt.Printf("v_connect() returned %d\n", socketNum)
		case "ls":
			if len(commands) != 1 {
				fmt.Print("Invalid number of arguments for ls")
				break
			}
			n.ListSocketCommand(w)
			w.Flush()
		case "sockets":
			if len(commands) != 1 {
				fmt.Print("Invalid number of arguments for ls")
				break
			}
			n.ListSocketCommand(w)
			w.Flush()
		case "s":
			if len(commands) != 3 {
				fmt.Print("Invalid number of arguments for s\n")
				break
			}
			n.SendTCPCommand(line)
		case "r":
			if len(commands) != 4 {
				fmt.Print("Invalid number of arguments for r\n")
				break
			}
			socketID, err := strconv.Atoi(commands[1])
			if err != nil {
				fmt.Printf("Invalid socket id number: %s\n", commands[1])
				break
			}
			numBytes, err := strconv.Atoi(commands[2])
			if err != nil {
				fmt.Printf("Invalid number of bytes: %s\n", commands[2])
				break
			}
			if commands[3] == "y" {
				err = n.ReadTCPCommand(socketID, uint32(numBytes), true)
			} else if commands[3] == "n" {
				err = n.ReadTCPCommand(socketID, uint32(numBytes), false)
			} else {
				fmt.Printf("Invalid option for read: %s\n", commands[3])
				break
			}
			if err != nil {
				fmt.Print(err)
			}
		case "cl":
			if len(commands) != 2 {
				fmt.Printf("Invalid number of arguments for cl\n")
				break
			}
			socketId, err := strconv.Atoi(commands[1])
			if err != nil {
				fmt.Printf("Invalid socket id number: %s\n", commands[1])
				break
			}
			err = n.CloseTCPCommand(socketId, true)
			if err != nil {
				fmt.Print(err)
			}
		case "sd":
			if len(commands) != 3 {
				fmt.Printf("Invalid number of arguments for sd\n")
				break
			}
			socketId, err := strconv.Atoi(commands[1])
			if err != nil {
				fmt.Printf("Invalid socket id number: %s\n", commands[1])
				break
			}
			err = n.ShutDownTCPCommand(socketId, commands[2])
			if err != nil {
				fmt.Print(err)
			}
		case "sf":
			if len(commands) != 4 {
				fmt.Printf("Invalid number of arguments for sf\n")
				break
			}
			port, err := strconv.Atoi(commands[3])
			if err != nil {
				fmt.Printf("Invalid port number\n")
			}
			err = n.SendFileTCPCommand(commands[1], commands[2], uint16(port))
			if err != nil {
				fmt.Print(err.Error())
			}
		case "rf":
			if len(commands) != 3 {
				fmt.Printf("Invalid number of arguments for rf\n")
				break
			}
			port, err := strconv.Atoi(commands[2])
			if err != nil {
				fmt.Printf("Invalid port number\n")
			}
			err = n.ReadFileTCPCommand(commands[1], uint16(port))
			if err != nil {
				fmt.Print(err.Error())
			}
		default:
			n.PrintHelp(w)
			w.Flush()
		}

		fmt.Print("> ")
	}
}

// run the node
func main() {

	args := os.Args

	if len(args) != 2 {
		log.Fatal("Usage: ./node <path to .lnx>")
	}

	filepath := args[1]
	node := Node{}
	node.StartNode(filepath)
}
