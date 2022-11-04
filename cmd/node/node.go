package main

import (
	"bufio"
	"encoding/binary"
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
)

// the node represents a machine that implements a host using IP routing and a TCP socket API built on top of the host
type Node struct {
	Host             *ip.Host // the node's IP host
	TCPHandler       *tcp.TCPHandler
	SocketIndexTable []tcp.Socket // essentially functions as a file descriptor table
}

func InitRoutingTable(localIFs map[uint32]*ip.LinkInterface) *ip.RoutingTable {
	routingTable := ip.RoutingTable{Table: make(map[uint32]*ip.RoutingTableEntry)}

	for localIFAddr := range localIFs {
		routingTable.Table[localIFAddr] = routingTable.CreateEntry(localIFAddr, 0)
	}
	return &routingTable
}

func NewRipHandler(msgChan chan []byte) ip.Handler {
	return &ip.RipHandler{MessageChan: msgChan}
}

func NewTracerouteHandler(nextHopMsg chan ip.NextHopMsg, echoChan chan []byte) ip.Handler {
	return &ip.TracerouteHandler{RouteChan: nextHopMsg, EchoChan: echoChan}
}

func NewTestHandler() ip.Handler {
	return &ip.TestHandler{}
}

// convert a uint32 ip addr to its string version
func addrNumToIP(addr uint32) string {
	return net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr)).String()
}

/*
	print out help usage for the node
*/
func (n *Node) PrintHelp(w io.Writer) {
	fmt.Fprintf(w, "send <ip> <proto> <string> \t - Sends the string payload to the given ip address with the specified protocol.\n")
	fmt.Fprintf(w, "down <interface-num> \t - Bring an interface \"down\".\n")
	fmt.Fprintf(w, "up <interface-num> \t - Bring an interface \"up\".\n")
	fmt.Fprintf(w, "interfaces, li <file> \t - Print information about each interface, one per line. Optionally specify a destination file.\n")
	fmt.Fprintf(w, "routes, lr <file>\t - Print information about the route to each known destination, one per line. Optionally specify a destination file.\n")
	fmt.Fprintf(w, "quit, q \t - Quit this node.\n")
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

func (n *Node) StartNode(filepath string) {

	f, err := os.Open(filepath)
	defer f.Close()

	if err != nil {
		log.Fatal(err)
	}

	// create host struct
	host := ip.Host{}
	// initialize host fields
	host.InitHost()

	scanner := bufio.NewScanner(f) // read from .lnx file:

	var hostConn *net.UDPConn

	l := 0
	for scanner.Scan() {
		line := strings.Split(scanner.Text(), " ")
		if l == 0 {
			// line 1:
			// set up our udp listener on the given host number
			if len(line) != 2 {
				log.Fatal("Ill formatted .lnx file, first line must be <host_addr> <host_port>")
			}
			listenString := fmt.Sprintf("%s:%s", line[0], line[1])

			listenAddr, err := net.ResolveUDPAddr("udp4", listenString)

			if err != nil {
				log.Fatal(err)
			}

			hostSocket, err := net.ListenUDP("udp4", listenAddr)

			if err != nil {
				log.Fatal(err)
			}

			// The socket that we will be listening to
			hostConn = hostSocket

			host.HostConnection = hostSocket
		} else {
			// line 2+

			// populate destination host, destination address, our link interface address fields
			// run init host function

			// add to host's dest addr to interface addr map
			// add to host's interface addr to interface struct map

			if len(line) != 4 {
				log.Fatal("Ill formatted .lnx file, " +
					"line must be <neighbor_addr> <neighbor_port> <host_interface_addr> <neighbor_interface_addr>")
			}

			hostIP := line[2]
			neighborIP := line[3]
			udpDestPort, err := strconv.Atoi(line[1])

			if err != nil {
				log.Print("Could not parse udp dest addr")
			}

			hostIPAddr := binary.BigEndian.Uint32(net.ParseIP(hostIP).To4())
			neighborIPAddr := binary.BigEndian.Uint32(net.ParseIP(neighborIP).To4())

			// create a LinkInterface for each line
			linkIF := ip.LinkInterface{
				InterfaceNumber: l - 1,
				HostConnection:  hostConn,
				HostIPAddress:   hostIPAddr,
				DestIPAddress:   neighborIPAddr,
				UDPDestAddr:     line[0],     // addr as string
				UDPDestPort:     udpDestPort, // udp dest port converted to int
				Stopped:         false,
				IPPacketChannel: host.PacketChannel,
			}

			// log.Printf("parsed udp addr: %s", linkIF.UDPDestAddr)

			host.LocalIFs[hostIPAddr] = &linkIF

			host.RemoteDestination[neighborIPAddr] = hostIPAddr

			if linkIF.InitializeDestConnection(line[0], line[1]) != nil { // addr and port as both strings
				log.Print("could not initialize interface connection")
				continue
			}
		}
		l++
	}

	// initailize and fill routing table with our information
	routingTable := InitRoutingTable(host.LocalIFs)
	host.RoutingTable = routingTable

	// register application handlers
	ripHandler := NewRipHandler(host.MessageChannel)
	testHandler := NewTestHandler()
	testHandler.InitHandler(nil)

	// initialize the traceroute handler, pass in the channel to be used
	tracerouteHandler := NewTracerouteHandler(host.NextHopChannel, host.EchoChannel)

	host.RegisterHandler(RIP_PROTOCOL, ripHandler) //
	host.RegisterHandler(TEST_PROTOCOL, testHandler)
	host.RegisterHandler(ICMP_PROTOCOL, tracerouteHandler)

	dataForHandler := make([]interface{}, 0)
	// sending both the routingTable and the RemoteDestinations as that contains the neighbors
	dataForHandler = append(dataForHandler, routingTable, &host.RemoteDestination, host.LocalIFs)
	go ripHandler.InitHandler(dataForHandler)

	// start listening for the host
	host.StartHost()

	// start CLI
	scanner = bufio.NewScanner(os.Stdin)

	// for table input
	w := new(tabwriter.Writer)
	// minwidth, tabwidth, padding, padchar, flags
	w.Init(os.Stdout, 16, 10, 0, '\t', 0)

	fmt.Print("> ")
	for scanner.Scan() {
		line := scanner.Text()
		if line[0] == '\n' {
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
			err = host.DownInterface(ifNum)

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

			err = host.UpInterface(ifNum)

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
			n.AcceptCommand()
		case "c":
			if len(commands) != 3 {
				fmt.Print("Invalid number of arguments for <c>onnect")
			}
			n.ConnectCommand()
		default:
			n.PrintHelp(w)
			w.Flush()
		}

		fmt.Print("> ")
	}
}

// where everything should be initialized
func main() {

	args := os.Args

	if len(args) != 2 {
		log.Fatal("Usage: ./node <path to .lnx>")
	}

	filepath := args[1]

	node := Node{}

	node.StartNode(filepath)

}
