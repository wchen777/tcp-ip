package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"tcp-ip/pkg/ip"
)

/*
	send an ip packet to a dest address, w/ given protocol num, and payload
*/
func (n *Node) SendCommand(line string) {
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

	err = n.Host.SendPacket(destAddr, protocolNum, args[3])

	if err != nil {
		log.Print(fmt.Sprintf("Cannot send to %s, error: %s", ipAddr.String(), err.Error()))
	}
}

/*
	Routine for printing out the active interfaces (li, interfaces)
*/
func (n *Node) PrintInterfaces(w io.Writer) {
	fmt.Fprintf(w, "id\t  state\t  local\t  remote\n")
	for addrLocalIF, localIF := range n.Host.LocalIFs {
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
	Routine for printing out the routing table (lr, routes)
*/
func (n *Node) PrintRoutingTable(w io.Writer) {
	fmt.Fprintf(w, "dest\t  next\t  cost\n")
	for dest, entry := range n.Host.RoutingTable.Table {
		destAddr := addrNumToIP(dest)
		nextHop := entry.NextHop
		if entry.Cost == ip.INFINITY {
			continue
		}
		nextHopAddr := addrNumToIP(nextHop)
		fmt.Fprintf(w, "%s\t  %s\t  %d\n", destAddr, nextHopAddr, entry.Cost)
	}
}

/*
	traceroute commmand to print out the shortest route
*/
func (n *Node) Traceroute(destAddr string) {
	destIPAddr := net.ParseIP(destAddr)
	if destIPAddr == nil {
		log.Print("The destination address is invalid")
		return
	}

	if localIF, exists := n.Host.LocalIFs[binary.BigEndian.Uint32(destIPAddr.To4())]; exists {
		// if it exists, then there's no need to send a packet
		// we can just special case this, and return 0 hops
		localIF.StoppedLock.Lock()
		if localIF.Stopped {
			// in the event that the local interface isn't available
			fmt.Printf("Traceroute to %s does not exist\n", destAddr)
		} else {
			fmt.Printf("Traceroute finished in 0 hops\n")
		}
		localIF.StoppedLock.Unlock()
		return
	}

	// make a goroutine that will handle this from the host end
	path := n.Host.SendTraceroutePacket(binary.BigEndian.Uint32(destIPAddr.To4()))

	// wait on a channel that should return the path
	// the channel data should be sent from the handler
	if len(path) == 0 {
		fmt.Printf("Traceroute to %s does not exist\n", destAddr)
	} else {
		startAddr := n.Host.RemoteDestination[path[0]]
		fmt.Printf("Traceroute from %s to %s\n", addrNumToIP(startAddr), destAddr)
		for index, hop := range path {
			hopAddr := addrNumToIP(hop)
			fmt.Printf("%d  %s\n", index+1, hopAddr)
		}
		fmt.Printf("Traceroute finished in %d hops\n", len(path))
	}
}
