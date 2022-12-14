package tcp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	"github.com/google/netstack/tcpip/header"
)

// need retransmission queue (TCB level)
// keep track of how many times we have retransmitted

// need to reset retransmit timer on packet send and retransmit packet send

const (
	ALPHA = 0.85
	// RTO_UPPER           = time.Duration(time.Second * 60)
	RTO_UPPER           = time.Duration(1 * time.Second)
	RTO_LOWER           = time.Duration(1 * time.Millisecond)
	BETA                = 1.5
	MAX_RETRANSMISSIONS = 5
)

type RetransmitSegment struct {
	TCPHeader  *header.TCPFields
	PayloadLen uint32
}

// when we receive an ACK we need to remove it from the retransmission queue
func (t *TCPHandler) removeFromRetransmissionQueue(tcbEntry *TCB, ackNum uint32) {
	// assume that TCB is locked, and will exit with it locked.
	index_to_remove := -1
	for i, retransmitHeader := range tcbEntry.RetransmissionQueue {
		if retransmitHeader.TCPHeader.SeqNum < ackNum {
			// log.Printf("seq num that is less than ackNum: %d\n", retransmitHeader.TCPHeader.SeqNum)
			index_to_remove = i
		} else {
			// if it's greater than or equal to, then we break
			break
		}
	}
	if index_to_remove > -1 {
		// log.Printf("index to remove for retransmission queue: %d\n", index_to_remove)
		tcbEntry.RetransmissionQueue = tcbEntry.RetransmissionQueue[index_to_remove+1:]
		tcbEntry.RetransmitCounter = 0 // reset retransmission counter
	}
}

// on packet receive, used the updated time to calculate a new RTO
// assumes TCP lock is held coming into the function
func (t *TCPHandler) updateTimeout(tcbEntry *TCB, segmentReceivedNum uint32) {
	tcbEntry.TCBLock.Lock()

	if _, exists := tcbEntry.SegmentToTimestamp[segmentReceivedNum]; !exists { // ensure that the seq num is in the map
		tcbEntry.TCBLock.Unlock()
		return
	}

	RTT := time.Duration(time.Now().UnixNano() - tcbEntry.SegmentToTimestamp[segmentReceivedNum]) // round-trip time

	// once we calculate RTT, we can evict it from the map
	delete(tcbEntry.SegmentToTimestamp, segmentReceivedNum)

	tcbEntry.TCBLock.Unlock()

	// log.Printf("RTT calculated: %d\n", RTT)

	if tcbEntry.SRTT == -1 {
		// on initial packet send, we do not have a previous SRTT value, so use curr measured RTT for this case
		SRTT := calculateSRTT(RTT, RTT) // smoothed round-trip time
		tcbEntry.SRTT = SRTT
	} else {
		// otherwise, calculate SRTT with last SRTT and measured RTT
		SRTT := calculateSRTT(tcbEntry.SRTT, RTT) // smoothed round-trip time
		tcbEntry.SRTT = SRTT
	}

	//log.Printf("SRTT: %d\n", tcbEntry.SRTT)

	RTO := calculateRTO(tcbEntry.SRTT) // use SRTT to calc retransmission timeout (in time.duration)
	tcbEntry.RTO = RTO

	// log.Printf("retransmit counter: %d\n", tcbEntry.RetransmitCounter)
	tcbEntry.RTOTimeoutChan <- true

	// log.Print("reached end of update timeout")

}

// calculate SRTT function (SRTT should be equal to RTT on first packet sent)
// RTT and SRTT passed in as time.Duration (nanosecond count)
func calculateSRTT(SRTTlast time.Duration, RTT time.Duration) time.Duration {
	return time.Duration(int64((float64(SRTTlast) * ALPHA) + (float64(RTT) * (1 - ALPHA))))
}

// calculate RTO function (RTO should be equal to the RTO upper bound before the first packet is sent)
func calculateRTO(SRTT time.Duration) time.Duration {
	return time.Duration(int64(Min64f(float64(RTO_UPPER), Max64f(BETA*float64(SRTT), float64(RTO_LOWER)))))
}

func (t *TCPHandler) fastRetransmit(tcbEntry *TCB, socketData *SocketData) {
	// this should only be called in the event that three duplicate ACKs are received
	// in which case we retransmit the first in the retransmit queue
	// the TCBEntry is locked on entry from the receive function
	log.Print("Reached fast retransmit")
	t.AfterLoss(tcbEntry)

	tcbEntry.CurrentAckFreq = 0
	retransmitPacket := tcbEntry.RetransmissionQueue[0] // retransmit the first item on the queue

	// create packet to retransmit
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
	binary.Write(buf, binary.BigEndian, socketData.DestAddr)

	bufToSend := buf.Bytes()
	bufToSend = append(bufToSend, MarshallTCPHeader(retransmitPacket.TCPHeader, socketData.DestAddr)...)

	// Get the original payload from the buffer
	seq_num_index := SequenceToBufferInd(retransmitPacket.TCPHeader.SeqNum)

	payload := make([]byte, retransmitPacket.PayloadLen)

	if retransmitPacket.PayloadLen > 0 {
		// handle wrap around for retransmit packet in buffer
		if seq_num_index+retransmitPacket.PayloadLen > MAX_BUF_SIZE {
			firstCopy := MAX_BUF_SIZE - seq_num_index
			secondCopy := seq_num_index + retransmitPacket.PayloadLen - MAX_BUF_SIZE
			copy(payload, tcbEntry.SND.Buffer[seq_num_index:])
			copy(payload[firstCopy:], tcbEntry.SND.Buffer[:secondCopy])
		} else {
			copy(payload, tcbEntry.SND.Buffer[seq_num_index:seq_num_index+retransmitPacket.PayloadLen])
		}
	}

	tcbEntry.SegmentToTimestamp[seq_num_index+retransmitPacket.PayloadLen] = time.Now().UnixNano()
	tcbEntry.RTO *= 2 // exponential backoff

	// unlock mutex for TCB, on return we should just return from receive
	tcbEntry.TCBLock.Unlock()

	// extend the buffer that we are sending with the payload
	bufToSend = append(bufToSend, payload...)
	t.IPLayerChannel <- bufToSend

	tcbEntry.RTOTimeoutChan <- true
}

// goroutine function to check timeout
func (t *TCPHandler) waitTimeout(tcbEntry *TCB, socketData *SocketData) {
	timeout := time.After(tcbEntry.RTO)
	for {
		select {
		case <-tcbEntry.RTOTimeoutChan:
			// log.Print("resetting rto timeout")
			// resetting the timeout in this case, either we have received an ACK or we have sent a new segment
			timeout = time.After(tcbEntry.RTO)
		case <-timeout:
			// use the first element from the retransmission queue and send it to the IP layer
			// the element should only be removed once we have received an ACK for it
			// reset the timer when sending

			// only timeout if we have sent something, which means the retransmission queue is non-empty
			tcbEntry.TCBLock.Lock()

			// cancel timeout if reached a finalizing state
			if tcbEntry.State == TIME_WAIT || tcbEntry.State == CLOSED {
				// log.Print("exiting timeout due to time wait reached")
				tcbEntry.TCBLock.Unlock()
				return
			}

			if len(tcbEntry.RetransmissionQueue) == 0 {
				// log.Print("retransmission queue empty, resetting timeout")
				timeout = time.After(tcbEntry.RTO)
				tcbEntry.TCBLock.Unlock()
				break
			}

			// reached max retransmissions, go straight to close and perform cleanup
			if tcbEntry.RetransmitCounter == MAX_RETRANSMISSIONS {
				// what should be signaled in this case?
				// log.Print("reached max retransmissions for data")
				tcbEntry.State = CLOSED
				tcbEntry.TimeoutCancelled.Store(true)
				// signal variables
				tcbEntry.SND.WriteBlockedCond.Broadcast()
				tcbEntry.SND.SendBlockedCond.Signal()
				tcbEntry.RCV.ReadBlockedCond.Signal()
				tcbEntry.TCBLock.Unlock()

				// delete entry from socket table
				delete(t.SocketTable, *socketData)
				return
			}

			// log.Print("retransmitting data packet")
			if tcbEntry.CControlEnabled.Load() {
				// if congestion control is enabled, we want to make sure the window and slow
				// start threshold are updated
				t.AfterLoss(tcbEntry)
			}

			retransmitPacket := tcbEntry.RetransmissionQueue[0] // retransmit the first item on the queue
			tcbEntry.RetransmitCounter++

			// create packet to retransmit
			buf := &bytes.Buffer{}
			binary.Write(buf, binary.BigEndian, socketData.LocalAddr)
			binary.Write(buf, binary.BigEndian, socketData.DestAddr)

			bufToSend := buf.Bytes()
			bufToSend = append(bufToSend, MarshallTCPHeader(retransmitPacket.TCPHeader, socketData.DestAddr)...)

			// Get the original payload from the buffer
			seq_num_index := SequenceToBufferInd(retransmitPacket.TCPHeader.SeqNum)
			payload := make([]byte, retransmitPacket.PayloadLen)

			if retransmitPacket.PayloadLen > 0 {
				// handle wrap around for retransmit packet in buffer
				if seq_num_index+retransmitPacket.PayloadLen > MAX_BUF_SIZE {
					firstCopy := MAX_BUF_SIZE - seq_num_index
					secondCopy := seq_num_index + retransmitPacket.PayloadLen - MAX_BUF_SIZE
					copy(payload, tcbEntry.SND.Buffer[seq_num_index:])
					copy(payload[firstCopy:], tcbEntry.SND.Buffer[:secondCopy])
				} else {
					copy(payload, tcbEntry.SND.Buffer[seq_num_index:seq_num_index+retransmitPacket.PayloadLen])
				}
			}

			tcbEntry.SegmentToTimestamp[seq_num_index+retransmitPacket.PayloadLen] = time.Now().UnixNano()
			tcbEntry.RTO *= 2 // exponential backoff
			// log.Printf("reset value: %d\n", tcbEntry.RTO)

			// unlock mutex for TCB
			tcbEntry.TCBLock.Unlock()

			// extend the buffer that we are sending with the payload
			bufToSend = append(bufToSend, payload...)
			t.IPLayerChannel <- bufToSend
			// log.Print("returning after sending to ip layer")

			timeout = time.After(tcbEntry.RTO) // reset the timeout
		}
	}
}

// for handshake timeout
func (t *TCPHandler) WaitForAckReceiver(tcbEntry *TCB, key *SocketData) {
	timeout := time.After(RTO_UPPER)

	for {
		select {
		case <-timeout:
			// resend the SYN-ACK because we haven't received an ACK yet
			if tcbEntry.RetransmitCounter == MAX_RETRANSMISSIONS {
				tcbEntry.State = CLOSED
				fmt.Println("Handshake timed out")
				delete(t.SocketTable, *key)
				return
			}

			// log.Print("retransmitting syn ack")
			tcbEntry.RetransmitCounter++

			buf := &bytes.Buffer{}
			binary.Write(buf, binary.BigEndian, key.LocalAddr)
			binary.Write(buf, binary.BigEndian, key.DestAddr)
			tcpHeader := CreateTCPHeader(key.LocalAddr, key.DestAddr, key.LocalPort, key.DestPort, tcbEntry.SND.ISS,
				tcbEntry.RCV.NXT, header.TCPFlagSyn|header.TCPFlagAck, tcbEntry.RCV.WND, []byte{})
			bufToSend := buf.Bytes()

			bufToSend = append(bufToSend, MarshallTCPHeader(&tcpHeader, key.DestAddr)...)

			timeout = time.After(RTO_UPPER)

			// send to IP layer
			t.IPLayerChannel <- bufToSend
		case <-tcbEntry.RTOTimeoutChan:
			tcbEntry.RetransmitCounter = 0
			return
		}
	}
}
