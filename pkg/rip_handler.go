package pkg

import (
	"bytes"
	"encoding/binary"
	"log"
	"time"
)

type RipHandler struct {
	MessageChan   chan []byte
	Neighbors     []uint32
	OwnInterfaces map[uint32]*LinkInterface // TODO: temp solution
}

const (
	UPDATE_FREQ = 5  // seconds
	TIMEOUT     = 12 // seconds
	INFINITY    = 16
	MASK        = (1 << 32) - 1
)

func (r *RipHandler) ReceivePacket(packet IPPacket, data interface{}) {
	// log.Print("Reached RIP handler...")

	var table *RoutingTable

	if val, ok := data.(*RoutingTable); ok {
		table = val
	} else {
		log.Print("Unable to cast to routing table in RIP handler")
		return
	}

	// deserialize the data to be a RIP command
	packetDataBuf := bytes.NewReader(packet.Data)
	ripEntry := RIPMessage{}
	ripEntry.Entries = make([]RIPEntry, 0)
	binary.Read(packetDataBuf, binary.BigEndian, &ripEntry.Command)
	binary.Read(packetDataBuf, binary.BigEndian, &ripEntry.NumEntries)

	// TODO: does casting here cause any issues?
	// log.Printf("ENTRIES NUMBER: %d\n", int(ripEntry.NumEntries))
	for i := 0; i < int(ripEntry.NumEntries); i++ {
		entry := RIPEntry{}
		binary.Read(packetDataBuf, binary.BigEndian, &entry.Cost)
		binary.Read(packetDataBuf, binary.BigEndian, &entry.Address)
		binary.Read(packetDataBuf, binary.BigEndian, &entry.Mask)
		ripEntry.Entries = append(ripEntry.Entries, entry)
	}
	// log.Printf("number of entries that are actually processed: %d\n", len(ripEntry.Entries))

	// the next hop's destination address would be in the ip header
	// i.e. neighbor's destination address
	// TODO: this should be correct as our immediate neighbors are the ones
	//       sending a message to the node that is currently reading / handling this packet
	nextHop := binary.BigEndian.Uint32(packet.Header.Src.To4())
	updatedEntries := make([]RIPEntry, 0)

	for _, newEntry := range ripEntry.Entries {
		// log.Printf("Entry address: %d\n", newEntry.Address)
		oldEntry := table.CheckRoute(newEntry.Address)
		if oldEntry == nil {
			log.Print("creating entry in here")
			// if D isn't in the table, add <D, C, N>
			updateChan := make(chan bool)
			table.AddRoute(newEntry.Address, newEntry.Cost+1, nextHop, updateChan)
			// send new update to every neighbor except for the one we received message from
			updatedEntries = append(updatedEntries, newEntry)
			go r.waitForUpdates(newEntry.Address, updateChan, table)
		} else {
			// D --> destination address
			// C_old --> the cost
			// M --> the neighbor / next hop

			if newEntry.Cost == INFINITY {
				log.Print("received poisoned entry")
			}

			log.Printf("old entry's next hop: %d\n", oldEntry.NextHop)
			log.Printf("next hop: %d\n", nextHop)

			// If existing entry <D, C_old, M>
			if (newEntry.Cost+1 < oldEntry.Cost) || (newEntry.Cost+1 > oldEntry.Cost && oldEntry.NextHop == nextHop) {
				log.Print("updating existing entry")
				// if C < C_old, update table <D, C, N> --> found better route
				// if C > C_old and N == M, update table <D, C, M> --> increased cost

				table.UpdateRoute(newEntry.Address, newEntry.Cost+1, nextHop)
				updatedEntries = append(updatedEntries, newEntry)
			}
			// if C == C_old, just refresh timer --> nothing new
			if _, ok := r.OwnInterfaces[newEntry.Address]; !ok {
				oldEntry.UpdateChan <- true
			}
		}
	}

	// Send an update to neighbors the entries that have changed
	if len(updatedEntries) > 0 {
		go r.SendTriggeredUpdates(updatedEntries, table)
	}
}

func (r *RipHandler) InitHandler(data []interface{}) {
	// send updates to all neighbors with entries from its routing table
	if len(data) != 3 {
		log.Print("Incorrect length of data, returning from init handler")
		return
	}

	var table *RoutingTable

	if val, ok := data[0].(*RoutingTable); ok {
		table = val
	} else {
		log.Print("Unable to create routing table from data")
		return
	}

	var neighborMap map[uint32]uint32

	if val, ok := data[1].(*map[uint32]uint32); !ok {
		log.Print("Unable to create list of neighbors from data")
		return
	} else {
		neighborMap = *val
	}

	var ownInterfaces map[uint32]*LinkInterface

	if val, ok := data[2].(map[uint32]*LinkInterface); !ok {
		log.Print("error")
		return
	} else {
		ownInterfaces = val
		r.OwnInterfaces = ownInterfaces
	}

	r.Neighbors = make([]uint32, 0)
	for key := range neighborMap {
		r.Neighbors = append(r.Neighbors, key)
	}
	// log.Printf("NEIGHBORS: %v\n", r.Neighbors)

	// for
	// entries := r.GetAllEntries(table)
	// numEntries := len(entries)

	// newRIPMessage := RIPMessage{}
	// newRIPMessage.Command = 1
	// newRIPMessage.NumEntries = uint16(numEntries)
	// newRIPMessage.Entries = entries

	// bytesArray := &bytes.Buffer{}
	// binary.Write(bytesArray, binary.BigEndian, )
	// binary.Write(bytesArray, binary.BigEndian, newRIPMessage.Command)
	// binary.Write(bytesArray, binary.BigEndian, newRIPMessage.NumEntries)
	// binary.Write(bytesArray, binary.BigEndian, newRIPMessage.Entries)

	// send to channel that is shared with the host
	// r.MessageChan <- bytesArray.Bytes()
	go r.SendUpdatesToNeighbors(table)
}

func (r *RipHandler) GetAllEntries(table *RoutingTable) []RIPEntry {
	table.TableLock.Lock()
	defer table.TableLock.Unlock()
	entries := make([]RIPEntry, 0)

	for destination, entry := range table.Table {
		ripEntry := RIPEntry{entry.Cost, destination, MASK}
		entries = append(entries, ripEntry)
	}

	return entries
}

func (r *RipHandler) GetSpecificEntries(table *RoutingTable, neighborToPoison uint32) []RIPEntry {
	table.TableLock.Lock()
	defer table.TableLock.Unlock()
	entries := make([]RIPEntry, 0)

	for destination, entry := range table.Table {
		ripEntry := RIPEntry{}
		ripEntry.Address = destination
		ripEntry.Mask = MASK

		if entry.NextHop == neighborToPoison {
			ripEntry.Cost = INFINITY
		} else {
			ripEntry.Cost = entry.Cost
		}
		// add entries to list
		entries = append(entries, ripEntry)
	}
	return entries
}

// function that will periodically send updates to routing table
// this should be where poisoned updates happen
func (r *RipHandler) SendUpdatesToNeighbors(table *RoutingTable) {
	timer := time.NewTicker(UPDATE_FREQ * time.Second)

	for {
		select {
		case <-timer.C:
			// send updates to all neighbors with entries from its routing table
			for _, neighbor := range r.Neighbors {
				// log.Print("Sending updates to neighbors now")
				// get routing table entries specific to a particular neighbor
				// the cost needs to be poisoned with INFINITY

				// TODO: decide if the rest of this code here should be a go routine or not
				entries := r.GetSpecificEntries(table, neighbor)

				numEntries := len(entries)
				newRIPMessage := RIPMessage{}
				newRIPMessage.Command = 1
				newRIPMessage.NumEntries = uint16(numEntries)
				newRIPMessage.Entries = entries

				bytesArray := &bytes.Buffer{}
				// the first four bytes should be the ip address of the neighbor
				// log.Printf("DESTINATION ADDR 1: %d\n", neighbor)
				binary.Write(bytesArray, binary.BigEndian, neighbor)
				binary.Write(bytesArray, binary.BigEndian, newRIPMessage.Command)
				binary.Write(bytesArray, binary.BigEndian, newRIPMessage.NumEntries)
				binary.Write(bytesArray, binary.BigEndian, newRIPMessage.Entries)

				// send to channel that is shared with the host
				r.MessageChan <- bytesArray.Bytes()
				// log.Print("Finished sending to host")
			}
		}
	}
}

// should probably protect the routing table if it's shared by more than one go routine

// This is for triggered updates upon updates to our routing table
func (r *RipHandler) SendTriggeredUpdates(entriesToSend []RIPEntry, table *RoutingTable) {
	// iterate through the map and send to every entry including the nextHop / ReceivedFrom address,
	// we should instead send INFINITY
	for _, neighbor := range r.Neighbors {
		// log.Print("here lol 2")
		// iterate through the immediate neighbors to send only the updates to routing table
		for i, entry := range entriesToSend {
			newEntry := table.CheckRoute(entry.Address)
			if newEntry == nil {
				// log.Printf("could not find entry in table w/ address: %v\n", entry.Address)
			} else if newEntry.NextHop == neighbor {
				// if the new entry's next hop is the neighbor that
				// we are sending the new updates to, then we should set the cost to be infinity
				entriesToSend[i].Cost = INFINITY
			}
		}

		// send to one neighbor
		newRIPMessage := RIPMessage{}
		newRIPMessage.Command = 1
		newRIPMessage.NumEntries = uint16(len(entriesToSend))
		newRIPMessage.Entries = entriesToSend

		bytesArray := &bytes.Buffer{}

		// first write the address
		binary.Write(bytesArray, binary.BigEndian, neighbor)
		binary.Write(bytesArray, binary.BigEndian, newRIPMessage)

		// send to channel that is shared with the host
		r.MessageChan <- bytesArray.Bytes()
	}
	// log.Print("returning here")
	return
}

// called for each new entry that is created
// works with two channels: timeout channel which waits for 12 seconds
func (r *RipHandler) waitForUpdates(newEntryAddress uint32, updateChan chan bool, table *RoutingTable) {

	timeout := time.After(time.Duration(TIMEOUT * time.Second))
	for {
		select {
		case update := <-updateChan:
			// handling the message
			if update {
				// log.Printf("Updating the timeout: %d\n", newEntryAddress)
				timeout = time.After(time.Duration(TIMEOUT * time.Second))
			}
		case <-timeout:
			// If we have reached the timeout case, then we should remove the entry and return
			log.Printf("timeout: %d", newEntryAddress)
			table.RemoveRoute(newEntryAddress)
			return
		}
	}
}
