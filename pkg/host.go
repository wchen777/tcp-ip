package pkg

import (
	"encoding/binary"
	"log"
	"net"

	"github.com/google/netstack/tcpip/header"
	"golang.org/x/net/ipv4"
)

type Host struct {
	LocalIFs          map[uint32]*LinkInterface // only for the interfaces local to this host
	RemoteDestination map[uint32]uint32         // maps destination address to interface that we will send data to
	RoutingTable      *RoutingTable

	MessageChannel chan []byte // broadcast these messages
	PacketChannel  chan IPPacket

	HandlerRegistry map[int]Handler
}

const (
	IPV6 = 6
)

func computeChecksum(packet IPPacket) (uint16, error) {
	// recompute the checksum
	packet.Header.Checksum = 0
	headerBytes, err := packet.Header.Marshal()
	if err != nil {
		log.Print("Dropping packet because marshalling packet failed")
		return 0, err
	}

	return header.Checksum(headerBytes, 0), nil
}

func (h *Host) InitHost() {
	h.PacketChannel = make(chan IPPacket) // for receiving on link layer
	h.MessageChannel = make(chan []byte)  // for sending on link layer
	h.LocalIFs = make(map[uint32]*LinkInterface)
	h.HandlerRegistry = make(map[int]Handler)
	h.RemoteDestination = make(map[uint32]uint32)
}

/*
	general send function for the command in the driver
*/
func (h *Host) SendPacket(destAddr uint32, protocol int, data string) {
	// determine the next hop
	nextHop := h.RoutingTable.CheckRoute(destAddr).NextHop

	// lookup next hop address in remote destination map to find our interface address
	if addrOfInterface, exists := h.RemoteDestination[nextHop]; exists {
		// lookup correct interface from the address to send this packet on
		if localInterface, exists := h.LocalIFs[addrOfInterface]; exists {
			// create the packet and send to link layer
			packet := h.CreateIPPacket(addrOfInterface, destAddr, []byte(data))
			localInterface.Send(packet)
		}
	}
}

/*
	populate the handler registry with the given handler
*/
func (h *Host) RegisterHandler(protocolNum int, handler Handler) {
	h.HandlerRegistry[protocolNum] = handler
}

func (h *Host) SendToNeighbors(dest uint32, packet IPPacket) {

	newCheckSum, err := computeChecksum(packet)
	if err != nil {
		log.Print(err)
		return
	}
	packet.Header.Checksum = int(newCheckSum)

	// lookup next hop address in remote destination map to find our interface address
	if addrOfInterface, exists := h.RemoteDestination[dest]; exists {
		// lookup correct interface from the address to send this packet on
		if localInterface, exists := h.LocalIFs[addrOfInterface]; exists {
			localInterface.Send(packet)
		}
	}
}

/*
	This will read from each of the interfaces and send the messages
*/
func (h *Host) ReadFromHandler() {
	// TODO: figure out how to make this more generalizable
	//       in the event that there are other handlers
	for {
		select {
		case data := <-h.MessageChannel:
			//log.Printf("data received here: %v\n", data)

			if len(data) == 0 { // check for empty data
				break
			}

			// if we receive a message from the channel
			// send to all the neighbors
			// by using the remote destination map
			for dest, src := range h.RemoteDestination {
				// Create an IP packet here
				packet := h.CreateIPPacket(src, dest, data)
				// send to the link layer
				go h.SendToNeighbors(dest, packet)
			}
		}
	}
}

func (h *Host) CreateIPPacket(src uint32, dest uint32, data []byte) IPPacket {
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
		TTL:      32,
		Protocol: 200,
		Checksum: 0, // checksum will be computed in send link layer
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

			if newCheckSum != uint16(checkSum) {
				log.Print("Dropping packet: checksum failed")
				continue
			}

			// TTL --> if the `TTL == 0`, then the packet should be dropped
			if packet.Header.TTL == 0 {
				log.Print("Dropping packet: TTL == 0")
				continue
			}

			// Destination address --> if it's the node itself, the packet doesn't need to be forwarded,
			// and if the packet matches a network in the forwarding table, it should be sent on that interface.
			// And if all the above conditions are false, then sent to next hop.
			// And if the next hop doesn't exist, the packet is dropped.
			destAddr := binary.BigEndian.Uint32(packet.Header.Dst.To4())
			if _, exists := h.LocalIFs[destAddr]; exists {
				// This field should only be checked in the event that the packet has reached its destination
				// call the appropriate handler function, otherwise packet is "dropped"
				log.Printf("Protocol number: %d\n", packet.Header.Protocol)
				if handler, exists := h.HandlerRegistry[packet.Header.Protocol]; exists {
					go handler.ReceivePacket(packet, h.RoutingTable)
				} else {
					log.Print("Dropping packet: handler protocol not registered")
				}
			} else {
				// decrement TTL
				packet.Header.TTL -= 1

				// recompute checksum, look up where the packet needs to be sent, forward packet
				go h.SendToLinkLayer(destAddr, packet)
			}
		}
	}
}

/*
	general "send to link layer" function for a given ip packet
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
	nextHop := h.RoutingTable.CheckRoute(destAddr).NextHop

	// lookup next hop address in remote destination map to find our interface address
	if addrOfInterface, exists := h.RemoteDestination[nextHop]; exists {
		// lookup correct interface from the address to send this packet on
		if localInterface, exists := h.LocalIFs[addrOfInterface]; exists {
			localInterface.Send(packet)
		}
	}
}

/*
	host's startup function, listen on all interfaces for incoming messages
*/
func (h *Host) StartHost() {
	// loop through all host interfaces and start goroutine for listen
	for _, linkIF := range h.LocalIFs {
		go linkIF.Listen()
	}

	// start goroutine for read from link layer
	go h.ReadFromLinkLayer()
	go h.ReadFromHandler()
}
