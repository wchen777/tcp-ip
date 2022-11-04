package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"tcp-ip/pkg/ip"
	"tcp-ip/pkg/tcp"
)

// convert a uint32 ip addr to its string version
func addrNumToIP(addr uint32) string {
	return net.IPv4(byte(addr>>24), byte(addr>>16), byte(addr>>8), byte(addr)).String()
}

/*
	initialize and return a routing table for the ip host
*/
func InitRoutingTable(localIFs map[uint32]*ip.LinkInterface) *ip.RoutingTable {
	routingTable := ip.RoutingTable{Table: make(map[uint32]*ip.RoutingTableEntry)}

	for localIFAddr := range localIFs {
		routingTable.Table[localIFAddr] = routingTable.CreateEntry(localIFAddr, 0)
	}
	return &routingTable
}

/*
	initialize a socket index table for the node
*/
func (n *Node) InitSocketIndexTable() {
	n.SocketIndexTable = make([]tcp.Socket, 0)
}

/*
	protocol handler for TCP
*/
func NewTCPHandler(IPLayerChan chan []byte) ip.Handler {
	return &tcp.TCPHandler{IPLayerChannel: IPLayerChan}
}

/*
	protocol handler for IP's RIP
*/
func NewRipHandler(msgChan chan []byte) ip.Handler {
	return &ip.RipHandler{MessageChan: msgChan}
}

/*
	protocol handler for traceroute
*/
func NewTracerouteHandler(nextHopMsg chan ip.NextHopMsg, echoChan chan []byte) ip.Handler {
	return &ip.TracerouteHandler{RouteChan: nextHopMsg, EchoChan: echoChan}
}

/*
	protocol handler for ip test
*/
func NewTestHandler() ip.Handler {
	return &ip.TestHandler{}
}

/*
	reads the first line of the .lnx file to setup the host's udp conn
*/
func (n *Node) SetupHost(line []string) {
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
	n.Host.HostConnection = hostSocket
}

/*
	read the 2+'th line of the .lnx file to initialize the ip node's interfaces
*/
func (n *Node) SetupInterface(line []string, l int) {
	// STEPS:
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
		HostConnection:  n.Host.HostConnection,
		HostIPAddress:   hostIPAddr,
		DestIPAddress:   neighborIPAddr,
		UDPDestAddr:     line[0],     // addr as string
		UDPDestPort:     udpDestPort, // udp dest port converted to int
		Stopped:         false,
		IPPacketChannel: n.Host.PacketChannel,
	}

	// log.Printf("parsed udp addr: %s", linkIF.UDPDestAddr)

	n.Host.LocalIFs[hostIPAddr] = &linkIF

	n.Host.RemoteDestination[neighborIPAddr] = hostIPAddr

	if linkIF.InitializeDestConnection(line[0], line[1]) != nil { // addr and port as both strings
		log.Print("could not initialize interface connection")
	}
}

/*
	setup handlers, RIP, traceroute, test
*/
func (n *Node) SetupHandlers() {

	// test handler init
	testHandler := NewTestHandler()
	testHandler.InitHandler(nil)

	// initialize and fill routing table with our information
	routingTable := InitRoutingTable(n.Host.LocalIFs)
	n.Host.RoutingTable = routingTable

	// register application handlers
	ripHandler := NewRipHandler(n.Host.MessageChannel)

	// initialize the traceroute handler, pass in the channel to be used
	tracerouteHandler := NewTracerouteHandler(n.Host.NextHopChannel, n.Host.EchoChannel)

	// initialize TCP header
	n.InitSocketIndexTable() // for tcp sockets
	tcpHandler := NewTCPHandler(n.Host.TCPMessageChannel)

	// register the handlers as functions for the host
	n.Host.RegisterHandler(RIP_PROTOCOL, ripHandler)         // 200
	n.Host.RegisterHandler(TEST_PROTOCOL, testHandler)       // 0
	n.Host.RegisterHandler(ICMP_PROTOCOL, tracerouteHandler) // 1
	n.Host.RegisterHandler(TCP_PROTOCOL, tcpHandler)         // 6

	// for rip handler
	dataForHandler := make([]interface{}, 0)

	// sending both the routingTable and the RemoteDestinations as that contains the neighbors
	dataForHandler = append(dataForHandler, routingTable, &n.Host.RemoteDestination, n.Host.LocalIFs)
	go ripHandler.InitHandler(dataForHandler)

	// tcp handler
	go tcpHandler.InitHandler(nil) // TODO: what else for TCP handler??
}

/*
	function to read in an lnx file and populate the necessary ip node fields and data structures
*/
func (n *Node) InitNodeFromLNX(filepath string) {
	f, err := os.Open(filepath)
	defer f.Close()

	if err != nil {
		log.Fatal(err)
	}

	// initialize host fields for the nod's host
	n.Host.InitHost()

	// read from .lnx file:
	scanner := bufio.NewScanner(f)

	l := 0
	for scanner.Scan() {
		line := strings.Split(scanner.Text(), " ")
		if l == 0 {
			// line 1:
			n.SetupHost(line)
		} else {
			// line 2+
			n.SetupInterface(line, l)
		}
		l++
	}

	// setup the protocol handler
	n.SetupHandlers()
}
