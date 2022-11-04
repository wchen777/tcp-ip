package main

import (
	"bufio"
	"fmt"
	"io"
	"log"
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

func (n *Node) REPL() {
	// start CLI
	scanner := bufio.NewScanner(os.Stdin)

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
