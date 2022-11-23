package tcp

import (
	"bytes"
	"encoding/binary"
	"time"

	"github.com/google/netstack/tcpip/header"
)

// need retransmission queue (TCB level)
// keep track of how many times we have retransmitted

// need to reset retransmit timer on packet send and retransmit packet send

const (
	ALPHA               = 0.85
	RTO_UPPER           = time.Duration(time.Second * 60)
	RTO_LOWER           = time.Duration(time.Second)
	BETA                = 1.5
	MAX_RETRANSMISSIONS = 3
)

type RetransmitSegment struct {
	TCPHeader  *header.TCPFields
	PayloadLen uint32
}

// when we receive an ACK we need to remove it from the retransmission queue
func (t *TCPHandler) removeFromRetransmissionQueue(tcbEntry *TCB, ackNum uint32) {
	// TODO: do we have the invariant that the first element is the one that we want to remove?
	// assume that TCB is locked, and will exit with it locked.
	index_to_remove := -1
	for i, retransmitHeader := range tcbEntry.RetransmissionQueue {
		if retransmitHeader.TCPHeader.SeqNum < ackNum {
			index_to_remove = i
		} else {
			// if it's greater than or equal to, then we break
			break
		}
	}
	// TODO: index_to_remove shouldn't be negative here
	tcbEntry.RetransmissionQueue = tcbEntry.RetransmissionQueue[index_to_remove+1:]
	tcbEntry.RetransmitCounter = 0 // reset retransmission counter
}

// on packet receive, used the updated
func (t *TCPHandler) updateTimeout(tcbEntry *TCB, segmentReceivedNum uint32) {
	// assume TCB is locked here, and will exit with it locked.

	RTT := time.Now().Unix() - tcbEntry.SegmentToTimestamp[segmentReceivedNum] // round-trip time

	if tcbEntry.SRTT == -1 {
		// on initial packet send, we do not have a previous SRTT value, so use curr measured RTT for this case
		SRTT := calculateSRTT(time.Duration(RTT), RTT) // smoothed round-trip time
		tcbEntry.SRTT = SRTT
	} else {
		// otherwise, calculate SRTT with last SRTT and measured RTT
		SRTT := calculateSRTT(tcbEntry.SRTT, RTT) // smoothed round-trip time
		tcbEntry.SRTT = SRTT
	}

	RTO := calculateRTO(tcbEntry.SRTT) // use SRTT to calc retransmission timeout (in time.duration)
	tcbEntry.RTO = RTO
	tcbEntry.RTOTimeoutChan <- true
}

// calculate SRTT function (SRTT should be equal to RTT on first packet sent)
func calculateSRTT(SRTTlast time.Duration, RTT int64) time.Duration {
	return time.Duration((SRTTlast.Seconds() * ALPHA) + (float64(RTT) * (1 - ALPHA)))
}

// calculate RTO function (RTO should be equal to the RTO upper bound before the first packet is sent)
func calculateRTO(SRTT time.Duration) time.Duration {
	return time.Duration(Min64f(RTO_UPPER.Seconds(), Max64f(BETA*SRTT.Seconds(), RTO_LOWER.Seconds())))
}

// goroutine function to check timeout
// TODO: where should we first call this routine?
func (t *TCPHandler) waitTimeout(tcbEntry *TCB, socketData *SocketData) {
	timeout := time.After(time.Duration(tcbEntry.RTO * time.Second))
	for {
		select {
		case <-tcbEntry.RTOTimeoutChan:
			// resetting the timeout in this case, either we have received an ACK or we have sent a new segment
			timeout = time.After(time.Duration(tcbEntry.RTO * time.Second))
		case <-timeout:
			// use the first element from the retransmission queue and send it to the IP layer
			// the element should only be removed once we have received an ACK for it
			// reset the timer when sending

			// only timeout if we have sent something, which means the retransmission queue is non-empty
			if len(tcbEntry.RetransmissionQueue) == 0 {
				timeout = time.After(time.Duration(tcbEntry.RTO * time.Second))
				continue
			}

			// reached max retransmissions, go straight to close and perform cleanup
			if tcbEntry.RetransmitCounter == MAX_RETRANSMISSIONS {
				// TODO: go directly into CLOSED?
				// what should be signaled in this case?
				tcbEntry.State = CLOSED
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

			// handle wrap around for retransmit packet in buffer
			if seq_num_index+retransmitPacket.PayloadLen > MAX_BUF_SIZE {
				firstCopy := MAX_BUF_SIZE - seq_num_index
				secondCopy := seq_num_index + retransmitPacket.PayloadLen - MAX_BUF_SIZE
				copy(payload, tcbEntry.SND.Buffer[seq_num_index:])
				copy(payload[firstCopy:], tcbEntry.SND.Buffer[:secondCopy])
			} else {
				copy(payload, tcbEntry.SND.Buffer[seq_num_index:seq_num_index+retransmitPacket.PayloadLen])
			}

			// extend the buffer that we are sending with the payload
			bufToSend = append(bufToSend, payload...)
			t.IPLayerChannel <- bufToSend
		}
	}
}
