package cmd

import (
	"bufio"
	"fmt"
	"ip/pkg"
	"log"
	"net"
	"os"
	"strings"
)

const (
	RIP_PROTOCOL  = 200
	TEST_PROTOCOL = 0
)

func InitRoutingTable() pkg.RoutingTable {
	return pkg.RoutingTable{
		Table: make(map[uint32]*pkg.RoutingTableEntry),
	}
}

func NewRipHandler(msgChan chan []byte) pkg.Handler {
	return &pkg.RipHandler{MessageChan: msgChan}
}

func NewTestHandler() pkg.Handler {
	return &pkg.TestHandler{}
}

// where everything should be initialized
// implement the CLI
func main() {

	args := os.Args

	if len(args) != 2 {
		fmt.Println("Usage: ./node <path to .lnx>")
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
			}

			host.LocalIFs[hostIP] = &linkIF             // TODO: convert host IP
			host.RemoteDestination[neighborIP] = hostIP // TODO: convert host IP and neighbor IP
		}

		l++
	}

	// register application handlers
	ripHandler := NewRipHandler(host.MessageChannel)
	testHandler := NewTestHandler()

	host.RegisterHandler(RIP_PROTOCOL, ripHandler)
	host.RegisterHandler(TEST_PROTOCOL, testHandler)

	// initailize and fill routing table with our information
	routingTable := InitRoutingTable()
	host.RoutingTable = &routingTable

	// start listening for the host
	host.StartHost()

	// start CLI
	scanner = bufio.NewScanner(os.Stdin)
	for {
		line := scanner.Text()
		commands := strings.Split(line, " ")

		switch commands[0] {
		case "interfaces":
			// information about interfaces
		case "li":
			// information about the interfaces
		case "routes":
		case "lr":
		case "down":
		case "up":
		case "send":
		}
	}

}
