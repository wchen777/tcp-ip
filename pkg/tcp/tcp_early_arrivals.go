package tcp

import "log"

type EarlyArrivalEntry struct {
	SequenceNum uint32
	PayloadLen  uint32
}

type EarlyArrivalQueue struct {
	EarlyArrivals []EarlyArrivalEntry
}

// want min heap to store sequence number boundaries for packets
// min-heap is sorted based off of starting sequence number

// early arrivals packets are copied into the buffer
// starting (sequence number, sequence number+payload) is stored into the min-heap

// when receiving a packet, check top of heap.
// store a "next ack num" variable, which is set to RCV.NXT
// if the top of heap's starting seq num is equal to "next ack num",
// move "next ack num" equal to the sequence number+payload of the heap's entry
// repeat prev 2 steps while the queue is empty, or the top of heap's starting seq num is not equal to "next ack num"
// send an ACK with "next ack num" as ACK num, update RCV.NXT to be this number, update window size to be prev window size - (this number - prev RCV.NXT)

// init heap
func (eq *EarlyArrivalQueue) Init() {
	eq.EarlyArrivals = make([]EarlyArrivalEntry, 0)
}

// is heap empty
func (eq *EarlyArrivalQueue) IsEmpty() bool {
	return len(eq.EarlyArrivals) == 0
}

// peek the top of the heap (but do not return it)
func (eq *EarlyArrivalQueue) Peek() EarlyArrivalEntry {
	return eq.EarlyArrivals[0]
}

// remove the top of the heap and return it
func (eq *EarlyArrivalQueue) Pop() EarlyArrivalEntry {
	poppedEntry := eq.EarlyArrivals[0]
	eq.EarlyArrivals = eq.EarlyArrivals[1:]
	return poppedEntry
}

// add an element to the heap
func (eq *EarlyArrivalQueue) Push(newEntry EarlyArrivalEntry) {
	// iterate through early arrivals queue
	log.Print("PUSHING ELEMENT TO EARLY ARRIVAL QUEUE")
	startingSeqNum := newEntry.SequenceNum
	insertIndex := -1
	// find position where the starting sequence number of the entry to push is less than the sequence number of the entry after it
	for i, earlyArrivalEntry := range eq.EarlyArrivals {
		if startingSeqNum+newEntry.PayloadLen <= earlyArrivalEntry.SequenceNum {
			insertIndex = i
		}
	}
	if insertIndex != -1 {
		rest := eq.EarlyArrivals[insertIndex:]      // start of list to where to insert
		beginning := eq.EarlyArrivals[:insertIndex] // end of list (after insert location)
		eq.EarlyArrivals = beginning
		eq.EarlyArrivals = append(eq.EarlyArrivals, newEntry) // append new entry
		eq.EarlyArrivals = append(eq.EarlyArrivals, rest...)  // rest of the list
	} else {
		// append at the end (means we did not find an entry that was less than ours at the list)
		eq.EarlyArrivals = append(eq.EarlyArrivals, newEntry)
	}
	log.Printf("last element: %d\n", newEntry.SequenceNum)
}

// pass in the value of RCV.NXT after receiving the in-order packet and copying data into buffer
// goal is to find the next ACK num to be sent if there exists early arrivals entries to be coalesced
// need to update window size outside of function ????
// return the ack num to be sent as a packet (RCV.NXT's new value)
func (t *TCPHandler) updateAckNum(tcbEntry *TCB, nextAck uint32) uint32 {

	entryNum := nextAck // the "next ack num"
	for !tcbEntry.EarlyArrivalQueue.IsEmpty() && entryNum == tcbEntry.EarlyArrivalQueue.Peek().SequenceNum {
		popped := tcbEntry.EarlyArrivalQueue.Pop()
		entryNum = popped.SequenceNum + popped.PayloadLen // update "next ack num" if found contiguous segment
	}
	log.Printf("updated ACK num: %d\n, in-order ACK num: %d\n", entryNum, nextAck)
	return entryNum
}
