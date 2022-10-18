package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"ip/pkg"
	"log"
	"net"
	"os"
	"strings"
	"text/tabwriter"
)

const (
	RIP_PROTOCOL  = 200
	TEST_PROTOCOL = 0
)

func InitRoutingTable(localIFs map[uint32]*pkg.LinkInterface) *pkg.RoutingTable {
	routingTable := pkg.RoutingTable{Table: make(map[uint32]*pkg.RoutingTableEntry)}

	for localIFAddr := range localIFs {
		routingTable.Table[localIFAddr] = routingTable.CreateEntry(localIFAddr, 0)
	}
	return &routingTable
}

func NewRipHandler(msgChan chan []byte) pkg.Handler {
	return &pkg.RipHandler{MessageChan: msgChan}
}

func NewTestHandler() pkg.Handler {
	return &pkg.TestHandler{}
}

/*
	Routine for printing out the active interfaces
*/
func printInterfaces(h *pkg.Host) {
	fmt.Printf("id  state  local  remote\n")
	for addrLocalIF, localIF := range h.LocalIFs {
		addrLocal := net.IPv4(byte(addrLocalIF>>24), byte(addrLocalIF>>16), byte(addrLocalIF>>8), byte(addrLocalIF)).String()
		if localIF.Stopped {
			fmt.Printf("%d  %s  %s  %s\n", localIF.InterfaceNumber, "down", addrLocal, localIF.UDPDestAddr)
		} else {
			fmt.Printf("%d  %s  %s  %s\n", localIF.InterfaceNumber, "up", addrLocal, localIF.UDPDestAddr)
		}
	}
}

/*
	Routine for printing out the routing table
*/
func printRoutingTable(h *pkg.Host) {
	fmt.Printf("dest  next  cost\n")
	for dest, entry := range h.RoutingTable.Table {
		destAddr := net.IPv4(byte(dest>>24), byte(dest>>16), byte(dest>>8), byte(dest)).String()
		nextHop := entry.NextHop
		nextHopAddr := net.IPv4(byte(nextHop>>24), byte(nextHop>>16), byte(nextHop>>8), byte(nextHop)).String()
		fmt.Printf("%s  %s   %d\n", destAddr, nextHopAddr, entry.Cost)
	}
}

/*
	print out help usage for the node
*/
func printHelp(w io.Writer) {
	fmt.Fprintf(w, "send <ip> <proto> <string> \t - Sends the string payload to the given ip address with the specified protocol.\n")
	fmt.Fprintf(w, "down <interface-num> \t - Bring an interface \"down\".\n")
	fmt.Fprintf(w, "up <interface-num> \t - Bring an interface \"up\".\n")
	fmt.Fprintf(w, "interfaces, li <file> \t - Print information about each interface, one per line. Optionally specify a destination file.\n")
	fmt.Fprintf(w, "routes, lr <file>\t - Print information about the route to each known destination, one per line. Optionally specify a destination file.\n")
	fmt.Fprintf(w, "quit, q \t - Quit this node.\n")
	fmt.Fprintf(w, "help, h \t - Show this help.\n")
}

// where everything should be initialized
func main() {

	args := os.Args

	if len(args) != 2 {
		log.Fatal("Usage: ./node <path to .lnx>")
	}

	filepath := args[1]

	f, err := os.Open(filepath)
	defer f.Close()

	if err != nil {
		log.Fatal(err)
	}

	// create host struct
	host := pkg.Host{}
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

			// create a LinkInterface for each line
			linkIF := pkg.LinkInterface{
				InterfaceNumber: l - 1,
				HostConnection:  hostConn,
				HostIPAddress:   hostIP,
				UDPDestAddr:     line[0],
				UDPDestPort:     line[1],
				Stopped:         false,
				IPPacketChannel: host.PacketChannel,
			}

			hostIPAddr := binary.BigEndian.Uint32(net.ParseIP(hostIP).To4())
			host.LocalIFs[hostIPAddr] = &linkIF

			neighborIPAddr := binary.BigEndian.Uint32(net.ParseIP(neighborIP).To4())
			host.RemoteDestination[neighborIPAddr] = hostIPAddr

			linkIF.InitializeHostConnection()
		}
		l++
	}

	// initailize and fill routing table with our information
	// TODO: doesn't look like information is being filled at the moment
	routingTable := InitRoutingTable(host.LocalIFs)
	host.RoutingTable = routingTable

	// register application handlers
	ripHandler := NewRipHandler(host.MessageChannel)
	testHandler := NewTestHandler()

	log.Print(host.RoutingTable.Table)
	host.RegisterHandler(RIP_PROTOCOL, ripHandler) //
	host.RegisterHandler(TEST_PROTOCOL, testHandler)
	log.Printf("In node.go: %T\n", routingTable)
	go ripHandler.InitHandler(routingTable)

	// start listening for the host
	host.StartHost()

	// start CLI
	scanner = bufio.NewScanner(os.Stdin)

	// for table input
	w := new(tabwriter.Writer)
	// minwidth, tabwidth, padding, padchar, flags
	w.Init(os.Stdout, 16, 10, 0, '\t', 0)

	for scanner.Scan() {
		fmt.Print("<")
		line := scanner.Text()
		commands := strings.Split(line, " ")

		switch commands[0] {
		case "interfaces":
			// information about interfaces
			printInterfaces(&host)
		case "li":
			// information about the interfaces
			printInterfaces(&host)
		case "routes":
			// routing table information
			printRoutingTable(&host)
		case "lr":
			// routing table information
			printRoutingTable(&host)
		case "down":
		case "up":
			if len(commands) < 2 {
				continue
			}
		case "send":
		case "help":
			printHelp(w)
			w.Flush()
		default:
			log.Printf("reached default case\n")
			printHelp(w)
			w.Flush()
		}
	}
}
