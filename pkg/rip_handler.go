package pkg

import (
	"log"
)

type RipHandler struct {
	ProtocolNumber int
}

func (r *RipHandler) ReceivePacket(packet IPPacket, table *RoutingTable) error {
	log.Print("Printing out packet data for RIP ...")
	log.Print(packet.Data)
	return nil

	// TODO:
	// CASES:
	// if D isn't in the table, add <D, C, N>
	// If existing entry <D, C_old, M>
	// if C < C_old, update table <D, C, N> --> found better route

	// if C > C_old and N == M, update table <D, C, M> --> increased cost

	// if C > C_old and N != M, ignore --> N is a better route

	// if C == C_old, refresh timer --> nothing new
}

// TODO: need function that will periodically send updates to routing table
// should probably protect the routing table if it's shared by more than one go routine
// TODO: need function that will send triggered updates when the routing tabel changes
