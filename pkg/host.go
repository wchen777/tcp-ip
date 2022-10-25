package pkg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"ip/pkg/traceroute"
	"log"
	"net"

	"github.com/google/netstack/tcpip/header"
	"golang.org/x/net/ipv4"
)

type Host struct {
	LocalIFs          map[uint32]*LinkInterface // only for the interfaces local to this host
	RemoteDestination map[uint32]uint32         // maps destination address to interface that we will send data to
	RoutingTable      *RoutingTable

	MessageChannel chan []byte // data from the rip handler
	PacketChannel  chan IPPacket
	NextHopChannel chan NextHopMsg // this is to pass to the icmp handler to properly communicate with the driver
	EchoChannel    chan []byte     // data from the traceroute handler

	HandlerRegistry map[int]Handler
	HostConnection  *net.UDPConn
}

const (
	IPV6         = 6
	ADDR_SIZE    = 4
	RIP_PROTOCOL = 200
	ICMP         = 1
	TTL_MAX      = 16
)

func computeChecksum(packet IPPacket) (uint16, error) {
	// recompute the checksum
	packet.Header.Checksum = 0
	headerBytes, err := packet.Header.Marshal()
	if err != nil {
		// TODO: make sure that we are error checking the return value
		log.Print("Dropping packet because marshalling packet failed")
		return 0, err
	}

	return header.Checksum(headerBytes, 0) ^ 0xffff, nil
}

func (h *Host) InitHost() {
	h.PacketChannel = make(chan IPPacket) // for receiving on link layer
	h.MessageChannel = make(chan []byte)  // for sending on link layer
	h.LocalIFs = make(map[uint32]*LinkInterface)
	h.HandlerRegistry = make(map[int]Handler)
	h.RemoteDestination = make(map[uint32]uint32)
	h.NextHopChannel = make(chan NextHopMsg)
	h.EchoChannel = make(chan []byte)
}

func (h *Host) CreateEchoPacket() traceroute.Echo {
	header := traceroute.ICMPHeader{
		Type:        uint8(8),
		Code:        uint8(0),
		Checksum:    uint16(0),
		Identifier:  uint16(0),
		SequenceNum: uint16(0),
	}
	return traceroute.Echo{
		Header: header,
		Data:   []byte("traceroute echo")}
}

// routine to send the packet
// list of destinations
func (h *Host) SendTraceroutePacket(destAddr uint32) []uint32 {
	// I think it's still going to be an IP packet, since the TTL value needs
	// to be checked
	entry := h.RoutingTable.CheckRoute(destAddr)
	if entry == nil {
		// sending empty list back to the driver
		return []uint32{}
	}

	h.HandlerRegistry[ICMP].AddChanRoutine()
	log.Printf("entry's next hop: %d\n", entry.NextHop)
	// we have the guarantee that the route should exist
	// start the ttl at 0
	currTTL := 1
	path := make([]uint32, 0)

	for currTTL <= INFINITY {
		nextHop := entry.NextHop

		// lookup next hop address in remote destination map to find our interface address
		if addrOfInterface, exists := h.RemoteDestination[nextHop]; exists {
			// lookup correct interface from the address to send this packet on
			if localInterface, exists := h.LocalIFs[addrOfInterface]; exists {
				// create the packet and send to link layer
				// TODO: not sure what to put for the protocol number when creating these packets
				echoMsg := h.CreateEchoPacket()
				bytesArray := &bytes.Buffer{}
				binary.Write(bytesArray, binary.BigEndian, echoMsg.Header)
				buf := bytesArray.Bytes()
				buf = append(buf, echoMsg.Data...)

				packet := h.CreateIPPacket(addrOfInterface, destAddr, buf, 1, currTTL)

				// make sure we are computing the checksum in this case too
				checkSum, _ := computeChecksum(packet)
				packet.Header.Checksum = int(checkSum)
				// log.Print("Sending echo packet now!!")
				localInterface.Send(packet)
			}
		}

		// after each message, wait for a response from ICMP handler
		select {
		case nextLoc := <-h.NextHopChannel:
			path = append(path, nextLoc.NextHop)
			// log.Print("finished appending path!!")
			if nextLoc.Found {
				h.HandlerRegistry[ICMP].RemoveChanRoutine()
				return path
			}
			break
		}
		currTTL += 1
	}
	return path
}

/*
	Routine for only listening for echo packets to forward
*/
func (h *Host) ReadEchoPacket() {
	for {
		select {
		case echoMsg := <-h.EchoChannel:
			// log.Print("reached here to send echo reply!")
			destAddr := binary.BigEndian.Uint32(echoMsg[:ADDR_SIZE])
			// log.Printf("destination address for echo reply: %d\n", destAddr)

			srcAddr := binary.BigEndian.Uint32(echoMsg[ADDR_SIZE : ADDR_SIZE+ADDR_SIZE])
			dataToSend := echoMsg[ADDR_SIZE+ADDR_SIZE:]

			entry := h.RoutingTable.CheckRoute(destAddr)
			nextHop := entry.NextHop

			// lookup next hop address in remote destination map to find our interface address
			if addrOfInterface, exists := h.RemoteDestination[nextHop]; exists {
				// lookup correct interface from the address to send this packet on
				if localInterface, exists := h.LocalIFs[addrOfInterface]; exists {
					// create the packet and send to link layer
					packet := h.CreateIPPacket(srcAddr, destAddr, []byte(dataToSend), 1, TTL_MAX)

					// make sure we are computing the checksum in this case too
					checkSum, _ := computeChecksum(packet)
					packet.Header.Checksum = int(checkSum)
					localInterface.Send(packet)
				}
			}
		}
	}
}

/*
	general send function for the command in the driver
*/
func (h *Host) SendPacket(destAddr uint32, protocol int, data string) error {

	// determine the next hop
	entry := h.RoutingTable.CheckRoute(destAddr)

	if entry == nil {
		return errors.New("Entry doesn't exist")
	}

	// if the cost of the entry that we're trying to send to is INFINITY, then we return because we can't send
	if entry.Cost == INFINITY {
		return errors.New("Unable to reach destination")
	}

	nextHop := entry.NextHop

	// check if we are sending to one of our interfaces
	if _, exists := h.LocalIFs[nextHop]; exists {
		packet := h.CreateIPPacket(nextHop, destAddr, []byte(data), 0, TTL_MAX)
		checkSum, _ := computeChecksum(packet)
		packet.Header.Checksum = int(checkSum)
		h.PacketChannel <- packet
		return nil
	}

	// lookup next hop address in remote destination map to find our interface address
	if addrOfInterface, exists := h.RemoteDestination[nextHop]; exists {
		// lookup correct interface from the address to send this packet on
		if localInterface, exists := h.LocalIFs[addrOfInterface]; exists {
			// create the packet and send to link layer
			packet := h.CreateIPPacket(addrOfInterface, destAddr, []byte(data), 0, TTL_MAX)

			// make sure we are computing the checksum in this case too
			checkSum, _ := computeChecksum(packet)
			packet.Header.Checksum = int(checkSum)
			localInterface.Send(packet)
		}
	}

	return nil
}

/*
	populate the handler registry with the given handler
*/
func (h *Host) RegisterHandler(protocolNum int, handler Handler) {
	h.HandlerRegistry[protocolNum] = handler
}

func (h *Host) SendToNeighbor(dest uint32, packet IPPacket) {

	newCheckSum, err := computeChecksum(packet)

	if err != nil {
		log.Print(err)
		return
	}

	packet.Header.Checksum = int(newCheckSum)

	// entry := h.RoutingTable.CheckRoute(dest)
	// if entry != nil && entry.Cost == INFINITY {
	// 	log.Print("Unable to reach destination because of cost infinity")
	// 	return
	// }

	// lookup next hop address in remote destination map to find our interface address
	if addrOfInterface, exists := h.RemoteDestination[dest]; exists {
		// lookup correct interface from the address to send this packet on
		if localInterface, exists := h.LocalIFs[addrOfInterface]; exists {
			localInterface.Send(packet)
		}
	}
}

/*
	This will read from the message channel for the rip handler and send the messages
*/
func (h *Host) ReadFromHandler() {
	// TODO: figure out how to make this more generalizable
	//       in the event that there are other handlers
	for {
		select {
		case data := <-h.MessageChannel:
			// if we receive a message from the channel, try and forward/send it to the correct interface
			if len(data) == 0 { // check for empty data
				break
			}
			// log.Print("Received data from rip handler channel")
			// get dest and src addr for the message
			destAddr := binary.BigEndian.Uint32(data[:ADDR_SIZE])
			// log.Printf("DESTINATION ADDR 2: %d\n", destAddr)
			srcAddr := h.RemoteDestination[destAddr]

			// Create an IP packet here
			packet := h.CreateIPPacket(srcAddr, destAddr, data[ADDR_SIZE:], RIP_PROTOCOL, TTL_MAX)

			// send to the link layer on the correct interface
			go h.SendToNeighbor(destAddr, packet)
		}
	}
}

func (h *Host) CreateIPPacket(src uint32, dest uint32, data []byte, protocol int, ttl int) IPPacket {
	srcAddress := net.IPv4(byte(src>>24), byte(src>>16), byte(src>>8), byte(src))
	destAddress := net.IPv4(byte(dest>>24), byte(dest>>16), byte(dest>>8), byte(dest))

	header := ipv4.Header{
		Version:  4,
		Len:      20, // Header length is always 20 when no IP options
		TOS:      0,
		TotalLen: ipv4.HeaderLen + len(data),
		ID:       0,
		Flags:    0,
		FragOff:  0,
		TTL:      ttl,
		Protocol: protocol, // no longer hardcoded
		Checksum: 0,        // checksum will be computed in send link layer
		Src:      srcAddress,
		Dst:      destAddress,
		Options:  []byte{},
	}

	return IPPacket{Header: header, Data: data}
}

func (h *Host) ReadFromLinkLayer() {
	// have a go routine per link interface that reads from the channel
	// and handles which handler should handle the message
	for {
		select {
		case packet := <-h.PacketChannel:
			// Verification checks for the ip packet

			// IP version --> if the version is IPv6, the packet should be dropped
			if packet.Header.Version == IPV6 {
				log.Print("Dropping packet: received IPV6 packet")
				continue
			}
			// Header Checksum --> if the checksum is not valid, the packet should also be dropped
			checkSum := packet.Header.Checksum
			newCheckSum, err := computeChecksum(packet)
			if err != nil {
				log.Print("Dropping packet because marshalling packet failed")
				continue
			}

			if int(newCheckSum) != checkSum {
				log.Printf("original check sum: %d\n", checkSum)
				log.Printf("new check sum: %d\n", newCheckSum)
				log.Print("Dropping packet: checksum failed")
				continue
			}

			// decrement TTL
			packet.Header.TTL -= 1

			// Destination address --> if it's the node itself, the packet doesn't need to be forwarded,
			// and if the packet matches a network in the forwarding table, it should be sent on that interface.
			// And if all the above conditions are false, then sent to next hop.
			// And if the next hop doesn't exist, the packet is dropped.
			destAddr := binary.BigEndian.Uint32(packet.Header.Dst.To4())
			if localInterface, exists := h.LocalIFs[destAddr]; exists { // REACHED ITS DESTINATION -- FOR US
				// This field should only be checked in the event that the packet has reached its destination
				// call the appropriate handler function, otherwise packet is "dropped"
				localInterface.StoppedLock.Lock()
				if localInterface.Stopped {
					// log.Printf("this local interface is stopped: %d\n", localInterface.HostIPAddress)
					localInterface.StoppedLock.Unlock()
					continue
				} else {
					localInterface.StoppedLock.Unlock()
				}
				if handler, exists := h.HandlerRegistry[packet.Header.Protocol]; exists {
					go handler.ReceivePacket(packet, h.RoutingTable)
				} else {
					log.Print("Dropping packet: handler protocol not registered")
				}
			} else { // FORWARD THE PACKET
				// TTL --> if the `TTL == 0`, then the packet should be dropped
				if packet.Header.TTL == 0 {
					// log.Print("Dropping packet: TTL == 0")
					// log.Print("Sending time exceeded back to the src")

					// call the function that will send time limit exceeded
					go h.sendICMPTimeExceeded(packet)
					continue
				}

				// recompute checksum, look up where the packet needs to be sent, forward packet
				go h.SendToLinkLayer(destAddr, packet)
			}
		}
	}
}

/*
	This will be called when TTL times out
*/
func (h *Host) sendICMPTimeExceeded(packet IPPacket) {
	// the sender of this packet that timed out
	originalSrc := binary.BigEndian.Uint32(packet.Header.Src.To4())

	// send a packet back to the original host
	header := traceroute.ICMPHeader{
		Type:        uint8(header.ICMPv4TimeExceeded),
		Code:        uint8(0),
		Checksum:    0, // TODO: add computation for checksum
		Identifier:  0,
		SequenceNum: 0,
	}
	msg := traceroute.TimeExceededMessage{
		Header:   header,
		IPHeader: packet.Header,
		Data:     packet.Data[0:8]}

	bytesArray := &bytes.Buffer{}
	binary.Write(bytesArray, binary.BigEndian, msg.Header)
	binary.Write(bytesArray, binary.BigEndian, msg.IPHeader)
	buf := bytesArray.Bytes()
	buf = append(buf, packet.Data[0:8]...)

	// the next hop to send the packet to
	entry := h.RoutingTable.CheckRoute(originalSrc)
	if entry == nil {
		return
	}
	if entry.Cost == INFINITY {
		log.Print("Unable to reach destination because of cost infinity")
		return
	}

	if addrOfInterface, exists := h.RemoteDestination[entry.NextHop]; exists {
		// addrOfInterface is the new source address
		packet := h.CreateIPPacket(addrOfInterface, originalSrc, buf, 1, 16)
		checkSum, _ := computeChecksum(packet)
		packet.Header.Checksum = int(checkSum)

		if localInterface, exists := h.LocalIFs[addrOfInterface]; exists {
			localInterface.Send(packet)
		} else {
			log.Printf("interface doesn't exist here to forward")
		}
	}
}

/*
	general "send to link layer" function for a given ip packet
	this is called when forwarding packets
*/
func (h *Host) SendToLinkLayer(destAddr uint32, packet IPPacket) {

	newCheckSum, err := computeChecksum(packet)
	if err != nil {
		log.Print(err)
		return
	}
	packet.Header.Checksum = int(newCheckSum)

	// This is where the routing table is consulted
	// hit routing table to find next hop address
	entry := h.RoutingTable.CheckRoute(destAddr)
	if entry == nil {
		log.Printf("Could not find entry in the routing table, dest addr: %d", destAddr)
		return
	}

	if entry.Cost == INFINITY {
		log.Print("Unable to reach destination because of cost infinity")
		return
	}

	nextHop := entry.NextHop
	// lookup next hop address in remote destination map to find our interface address
	if addrOfInterface, exists := h.RemoteDestination[nextHop]; exists {
		// lookup correct interface from the address to send this packet on
		if localInterface, exists := h.LocalIFs[addrOfInterface]; exists {
			localInterface.Send(packet)
		} else {
			log.Printf("interface doesn't exist here to forward")
		}
	} else {
		log.Printf("next hop doesn't exist to forward")
	}
}

func (h *Host) ListenOnPort() {
	for {
		// Take a look at sync.Cond, sync.WaitGroup
		// always listening for packets
		buffer := make([]byte, MTU)
		bytesRead, udpAddr, err := h.HostConnection.ReadFromUDP(buffer) // TODO: check address of sender ()
		if err != nil {
			log.Print(err)
		}

		found := false
		for _, localIF := range h.LocalIFs {
			if udpAddr.Port == localIF.UDPDestPort { // we received a packet from an unknown "port", (ports need to be int)
				found = true
				break
			}
		}

		if !found {
			log.Print("Could not find correct destination port")
			continue
		}

		// deserialize into IPPacket to return
		ipPacket := IPPacket{}
		hdr, _ := ipv4.ParseHeader(buffer)
		ipPacket.Header = *hdr
		ipPacket.Data = buffer[hdr.Len:bytesRead]

		// send to network layer from link layer
		h.PacketChannel <- ipPacket
	}
}

// functions to down a specific hosts interface
func (h *Host) DownInterface(interfaceNum int) error {

	for _, interf := range h.LocalIFs {
		if interf.InterfaceNumber == interfaceNum {

			if interf.Stopped {
				return errors.New(fmt.Sprintf("interface %d is already down", interfaceNum))
			}

			interf.Disable()

			// update the routing table
			// remove any entry that should go through the this interface
			neighbor := interf.DestIPAddress
			h.RoutingTable.RemoveNextHops([]uint32{interf.HostIPAddress, neighbor})
			log.Printf("interface %d is now down", interfaceNum)
			return nil
		}
	}

	return errors.New("could not find interface associated with this number")
}

// functions to up a specific host interface
func (h *Host) UpInterface(interfaceNum int) error {
	for _, interf := range h.LocalIFs {
		if interf.InterfaceNumber == interfaceNum {
			if !interf.Stopped {
				return errors.New(fmt.Sprintf("interface %d is already up", interfaceNum))
			}
			interf.Enable()

			// update the routing table with ourself first
			// the periodic updates function should propogate this entry
			h.RoutingTable.TableLock.Lock()
			h.RoutingTable.Table[interf.HostIPAddress] = h.RoutingTable.CreateEntry(interf.HostIPAddress, 0)
			h.RoutingTable.TableLock.Unlock()

			log.Printf("interface %d is back up", interfaceNum)
			return nil
		}
	}
	return errors.New("could not find interface associated with this number")
}

/*
	host's startup function, listen on all interfaces for incoming messages
*/
func (h *Host) StartHost() {
	// loop through all host interfaces and start goroutine for listen
	go h.ListenOnPort()

	// start goroutine for read from link layer
	go h.ReadFromLinkLayer()
	go h.ReadFromHandler()
	go h.ReadEchoPacket()
}
