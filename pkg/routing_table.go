package pkg

// Contains a mapping from the ip address (of the destination) to a link interfacce
// TODO: should the link interface should correspond to the destination or the src?
//       maybe we can have a struct that also contains the cost or encode it into the link interface
type RoutingTable struct {
	Table map[string]*LinkInterface
}

// TODO:
// checkRoute function --> gets the entry from the table when routing IP packets
// update function --> updates the routing table when a RIP packet is received
