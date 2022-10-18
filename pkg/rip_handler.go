package pkg

import (
	"bytes"
	"encoding/binary"
	"log"
	"time"
)

type RipHandler struct {
	MessageChan chan []byte
}

const (
	UPDATE_FREQ = 5  // seconds
	TIMEOUT     = 12 // seconds
	INFINITY    = 16
)

func (r *RipHandler) ReceivePacket(packet IPPacket, data interface{}) {
	log.Print("Printing out packet data for RIP ...")
	log.Print(packet.Data)

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
	binary.Read(packetDataBuf, binary.BigEndian, &ripEntry)

	// the next hop's destination address would be in the ip header
	// i.e. neighbor's destination address
	nextHop := binary.BigEndian.Uint32(packet.Header.Dst.To4())
	updatedEntries := make([]RIPEntry, 0)

	for _, newEntry := range ripEntry.Entries {
		oldEntry := table.CheckRoute(newEntry.Address)
		if oldEntry == nil {
			// if D isn't in the table, add <D, C, N>
			updateChan := make(chan bool, 1)
			table.AddRoute(newEntry.Address, newEntry.Cost+1, nextHop, updateChan)
			// send new update to every neighbor except for the one we received message from
			updatedEntries = append(updatedEntries, newEntry)
			r.waitForUpdates(newEntry.Address, updateChan, table)
		} else {
			// D --> destination address
			// C_old --> the cost
			// M --> the neighbor / next hop

			// If existing entry <D, C_old, M>
			if newEntry.Cost < oldEntry.Cost || newEntry.Cost > oldEntry.Cost && oldEntry.NextHop == nextHop {
				// if C < C_old, update table <D, C, N> --> found better route
				// if C > C_old and N == M, update table <D, C, M> --> increased cost
				table.UpdateRoute(newEntry.Address, newEntry.Cost, nextHop)
				updatedEntries = append(updatedEntries, newEntry)
			}

			// if C == C_old, just refresh timer --> nothing new
			oldEntry.UpdateChan <- true
		}
	}

	// Send an update to neighbors the entries that have changed
	r.SendTriggeredUpdates(nextHop, updatedEntries, table)
}

// TODO: modularize getting all entries
func (r *RipHandler) InitHandler(data interface{}) {
	// send updates to all neighbors with entries from its routing table
	var table *RoutingTable
	log.Printf("In rip_handler.go: %T\n", table)

	if val, ok := data.(*RoutingTable); ok {
		table = val
	} else {
		log.Print("Unable to create routing table from data")
		return
	}

	entries := r.GetAllEntries(table)
	numEntries := len(entries)

	newRIPMessage := RIPMessage{}
	newRIPMessage.Command = 1
	newRIPMessage.NumEntries = uint16(numEntries)
	newRIPMessage.Entries = entries

	bytesArray := &bytes.Buffer{}
	binary.Write(bytesArray, binary.BigEndian, newRIPMessage)

	// send to channel that is shared with the host
	r.MessageChan <- bytesArray.Bytes()
}

func (r *RipHandler) GetAllEntries(table *RoutingTable) []RIPEntry {
	table.TableLock.Lock()
	defer table.TableLock.Unlock()
	entries := make([]RIPEntry, 0)

	for destination, entry := range table.Table {
		ripEntry := RIPEntry{entry.Cost, destination, (1 << 32) - 1}
		entries = append(entries, ripEntry)
	}

	return entries
}

// function that will periodically send updates to routing table
func (r *RipHandler) SendUpdates(table *RoutingTable) {
	timer := time.NewTicker(UPDATE_FREQ * time.Second)

	for {

		select {
		case <-timer.C:
			// send updates to all neighbors with entries from its routing table
			entries := r.GetAllEntries(table)
			numEntries := len(entries)

			newRIPMessage := RIPMessage{}
			newRIPMessage.Command = 1
			newRIPMessage.NumEntries = uint16(numEntries)
			newRIPMessage.Entries = entries

			bytesArray := &bytes.Buffer{}
			binary.Write(bytesArray, binary.BigEndian, newRIPMessage)

			// send to channel that is shared with the host
			r.MessageChan <- bytesArray.Bytes()
		}
	}
}

// should probably protect the routing table if it's shared by more than one go routine

// This is for triggered updates upon updates to our routing table
func (r *RipHandler) SendTriggeredUpdates(ReceivedFrom uint32, entriesToSend []RIPEntry, table *RoutingTable) {
	// iterate through the map and send to every entry including the nextHop / ReceivedFrom address,
	// we should instead send INFINITY
	for i, entry := range entriesToSend {
		if table.CheckRoute(entry.Address).NextHop == ReceivedFrom {
			entriesToSend[i].Cost = INFINITY
		}
	}

	// send to host
	newRIPMessage := RIPMessage{}
	newRIPMessage.Command = 1
	newRIPMessage.NumEntries = uint16(len(entriesToSend))
	newRIPMessage.Entries = entriesToSend

	bytesArray := &bytes.Buffer{}
	binary.Write(bytesArray, binary.BigEndian, newRIPMessage)

	// send to channel that is shared with the host
	r.MessageChan <- bytesArray.Bytes()
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
				timeout = time.After(time.Duration(TIMEOUT * time.Second))
			}
		case <-timeout:
			// If we have reached the timeout case, then we should remove the entry and return
			table.RemoveRoute(newEntryAddress)
			return
		}
	}
}
