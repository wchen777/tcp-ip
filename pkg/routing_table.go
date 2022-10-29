package pkg

import (
	"sync"
)

// Contains a mapping from the ip address (of the destination) to a link interfacce
type RoutingTableEntry struct {
	NextHop    uint32
	Cost       uint32
	UpdateChan chan bool
}

type RoutingTable struct {
	Table     map[uint32]*RoutingTableEntry
	TableLock sync.Mutex
}

func (rt *RoutingTable) CreateEntry(nextHop uint32, cost uint32) *RoutingTableEntry {
	return &RoutingTableEntry{NextHop: nextHop, Cost: cost, UpdateChan: make(chan bool)}
}

// CheckRoute function --> gets the entry from the table when routing IP packets
func (rt *RoutingTable) CheckRoute(dest uint32) *RoutingTableEntry {
	rt.TableLock.Lock()
	defer rt.TableLock.Unlock()

	if entry, exists := rt.Table[dest]; exists {
		return entry
	} else {
		return nil
	}
}

// Update function --> updates the routing table when a RIP packet is received
func (rt *RoutingTable) UpdateRoute(dest uint32, newCost uint32, nextHop uint32) {
	rt.TableLock.Lock()
	defer rt.TableLock.Unlock()

	if entry, exists := rt.Table[dest]; exists {
		entry.Cost = newCost
		entry.NextHop = nextHop
	}
}

// AddRoute function --> adds a new route to the routing table
func (rt *RoutingTable) AddRoute(dest uint32, cost uint32, nextHop uint32, updateChan chan bool) {
	rt.TableLock.Lock()
	defer rt.TableLock.Unlock()

	rt.Table[dest] = &RoutingTableEntry{
		NextHop:    nextHop,
		Cost:       cost,
		UpdateChan: updateChan,
	}
}

func (rt *RoutingTable) RemoveNextHops(nextHops []uint32) {
	rt.TableLock.Lock()
	defer rt.TableLock.Unlock()
	// log.Print("pls be here")

	for dest, entry := range rt.Table {
		for _, nh := range nextHops {
			if nh == entry.NextHop {
				rt.Table[dest].Cost = INFINITY
			}
		}
	}
}

func (rt *RoutingTable) RemoveRoute(dest uint32) {
	rt.TableLock.Lock()
	defer rt.TableLock.Unlock()

	delete(rt.Table, dest)
}
