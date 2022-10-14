package pkg

import (
	"log"
)

type Host struct {
	HostName string
	LinkIFs  []*LinkInterface
	// IPAddr        string
	RoutingTable  *RoutingTable
	PacketChannel chan IPPacket // when to initialize this?
}

const (
	IPV6          = 6
	RIP_PROTOCOL  = 200
	TEST_PROTOCOL = 0
)

// TODO: figure out how to register handlers for the different protocols
func (h *Host) RegisterHandler(protocolNum int, handler Handler) {

}

func (h *Host) ReadFromLinkLayer() {
	// have a go routine per link interface that reads from the channel
	// and handles which handler should handle the message
	for {
		select {
		case packet := <-h.PacketChannel:
			log.Print("Logging packet header:")
			log.Print(packet.Header)
			// TODO: need to call the appropriate handler
			// TODO: verification checks for the ip packet
			// IP version --> if the version is IPv6, the packet should be dropped
			if packet.Header.Version == IPV6 {
				continue
			}
			// Header Checksum --> if the checksum is not valid, the packet should also be dropped
			if packet.Header.Checksum {

			}
			// TTL --> if the `TTL == 0`, then the packet should be dropped
			if packet.Header.TTL == 0 {
				continue
			}

			// Destination address --> if it's the node itself, the packet doesn't need to be forwarded, and if the packet matches a network in the forwarding table, it should be sent on that interface. And if all the above conditions are false, then sent to next hop. And if the next hop doesn't exist, the packet is dropped.
			if packet.Header.Dst.String() {

			}

			// This field should only be checked in the event that the packet has reached its destination
			if packet.header.Protocol == RIP_PROTOCOL {
				// RIP packet
			} else if packet.header.Protocol == TEST_PROTOCOL {
				// test packet
				test_handler
			}
		}
	}
}

func (h *Host) SendToLinkLayer() {
	// TODO: need to figure out which link layer that we need to send to
	// this is where the routing table is consulted

}
