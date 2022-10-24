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
	"strconv"
	"strings"
	"text/tabwriter"
)

const (
	RIP_PROTOCOL  = 200
	TEST_PROTOCOL = 0
	ICMP_PROTOCOL = 1
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

func NewTracerouteHandler(msgChan chan []uint32) pkg.Handler {
	return &pkg.TracerouteHandler{RouteChan: msgChan}
}

func NewTestHandler() pkg.Handler {
	return &pkg.TestHandler{}
}

// convert a uint32 ip addr to its string version
func addrNumToIP(addr uint32) string {
	return net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr)).String()
}

/*
	Routine for printing out the active interfaces
*/
func printInterfaces(h *pkg.Host, w io.Writer) {
	fmt.Fprintf(w, "id\t  state\t  local\t  remote\n")
	for addrLocalIF, localIF := range h.LocalIFs {
		addrLocal := addrNumToIP(addrLocalIF)
		addrRemote := addrNumToIP(localIF.DestIPAddress)
		if localIF.Stopped {
			fmt.Fprintf(w, "%d\t  %s\t  %s\t  %s\n", localIF.InterfaceNumber, "down", addrLocal, addrRemote)
		} else {
			fmt.Fprintf(w, "%d\t  %s\t  %s\t  %s\n", localIF.InterfaceNumber, "up", addrLocal, addrRemote)
		}
	}
}

/*
	Routine for printing out the routing table
*/
func printRoutingTable(h *pkg.Host, w io.Writer) {
	fmt.Fprintf(w, "dest\t  next\t  cost\n")
	for dest, entry := range h.RoutingTable.Table {
		destAddr := addrNumToIP(dest)
		nextHop := entry.NextHop
		if entry.Cost == pkg.INFINITY {
			continue
		}
		nextHopAddr := addrNumToIP(nextHop)
		fmt.Fprintf(w, "%s\t  %s\t  %d\n", destAddr, nextHopAddr, entry.Cost)
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

/*

 */
func sendCommand(h *pkg.Host, line string) {
	args := strings.SplitN(line, " ", 4)

	ipAddr := net.ParseIP(args[1])
	if ipAddr == nil {
		log.Print("Invalid ip address")
		return
	}
	destAddr := binary.BigEndian.Uint32(ipAddr.To4())
	protocolNum, err := strconv.Atoi(args[2])

	if err != nil || protocolNum != TEST_PROTOCOL {
		log.Print("Invalid protocol num")
		return
	}

	err = h.SendPacket(destAddr, protocolNum, args[3])

	if err != nil {
		addrNum, _ := strconv.Atoi(err.Error())
		addr := addrNumToIP(uint32(addrNum))
		log.Print(fmt.Sprintf("cannot send to %s, it is an unreachable address", addr))
	}
}

/*
	q command to quit out of REPL and clean up UDP sending connections
*/
func quit(h *pkg.Host) {
	for _, interf := range h.LocalIFs {
		interf.DestConnection.Close()
	}

	os.Exit(0)
}

/*
	traceroute commmand to print out the shortest route
*/
func traceroute(h *pkg.Host, destAddr string) {
	destIPAddr := net.ParseIP(destAddr)
	if destIPAddr == nil {
		log.Print("The destination address is invalid")
		return
	}

	pathToTake := make([]uint32, 0)

	// make a goroutine that will handle this from the host end
	path := h.SendTraceroutePacket(binary.BigEndian.Uint32(destIPAddr.To4()))

	// wait on a channel that should return the path
	// the channel data should be sent from the handler
	if len(path) == 0 {
		fmt.Printf("Traceroute to %s does not exists\n", destAddr)
	} else {
		startAddr := path[0]
		fmt.Printf("Traceroute from %s to %s\n", startAddr, destAddr)
		for index, hop := range pathToTake {
			hopAddr := addrNumToIP(hop)
			fmt.Printf("%d  %s\n", index+1, hopAddr)
		}
		fmt.Printf("Traceroute finished in %d hops\n", len(pathToTake))
	}
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
	//defer hostConn.Close()

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
			hostIPAddr := binary.BigEndian.Uint32(net.ParseIP(hostIP).To4())
			neighborIPAddr := binary.BigEndian.Uint32(net.ParseIP(neighborIP).To4())

			// create a LinkInterface for each line
			linkIF := pkg.LinkInterface{
				InterfaceNumber: l - 1,
				HostConnection:  hostConn,
				HostIPAddress:   hostIPAddr,
				DestIPAddress:   neighborIPAddr,
				UDPDestAddr:     line[0],
				UDPDestPort:     line[1],
				Stopped:         false,
				IPPacketChannel: host.PacketChannel,
			}

			// log.Printf("parsed udp addr: %s", linkIF.UDPDestAddr)

			host.LocalIFs[hostIPAddr] = &linkIF

			host.RemoteDestination[neighborIPAddr] = hostIPAddr

			linkIF.InitializeDestConnection()
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
	tracerouteHandler := NewTracerouteHandler(host.ICMPChannel)

	host.RegisterHandler(RIP_PROTOCOL, ripHandler) //
	host.RegisterHandler(TEST_PROTOCOL, testHandler)
	host.RegisterHandler(ICMP_PROTOCOL, tracerouteHandler)

	dataForHandler := make([]interface{}, 0)
	// sending both the routingTable and the RemoteDestinations as that contains the neighbors
	// TODO: temp solution
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
		if line == "\n" {
			continue
		}
		commands := strings.Split(line, " ")

		switch commands[0] {
		case "interfaces":
			// information about interfaces
			printInterfaces(&host, w)
			w.Flush()
			break
		case "li":
			// information about interfaces
			printInterfaces(&host, w)
			w.Flush()
			break
		case "routes":
			// routing table information
			printRoutingTable(&host, w)
			w.Flush()
			break
		case "lr":
			// routing table information
			printRoutingTable(&host, w)
			w.Flush()
			break
		case "down":
			if len(commands) < 2 {
				log.Print("Invalid number of arguments for down")
				break
			}
			ifNum, err := strconv.Atoi(commands[1])
			if err != nil {
				log.Print("Invalid input for interface number")
				break
			}
			err = host.DownInterface(ifNum)

			if err != nil {
				log.Print(err.Error())
			}
		case "up":
			if len(commands) < 2 {
				log.Print("Invalid number of arguments for up")
				break
			}

			ifNum, err := strconv.Atoi(commands[1])
			if err != nil {
				log.Print("Invalid input for interface number")
				break
			}

			err = host.UpInterface(ifNum)

			if err != nil {
				log.Print(err.Error())
			}

		case "send":
			if len(commands) < 4 {
				log.Print("Invalid number of arguments for send")
				break
			}
			sendCommand(&host, line)
			break
		case "q":
			if len(commands) != 1 {
				log.Print("Invalid number of arguments for q")
				break
			}
			quit(&host)
		case "traceroute":
			if len(commands) != 2 {
				log.Print("Invalid number of arguments for traceroute")
				break
			}
			traceroute(&host, commands[1])
		default:
			printHelp(w)
			w.Flush()
		}

		fmt.Print("> ")
	}
}
