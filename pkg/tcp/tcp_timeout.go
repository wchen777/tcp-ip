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
	RTO_UPPER           = time.Duration(10 * time.Second)
	RTO_LOWER           = time.Duration(1 * time.Millisecond)
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
			log.Printf("seq num that is less than ackNum: %d\n", retransmitHeader.TCPHeader.SeqNum)
			index_to_remove = i
		} else {
			// if it's greater than or equal to, then we break
			break
		}
	}
	// TODO: index_to_remove shouldn't be negative here
	if index_to_remove > -1 {
		log.Printf("index to remove for retransmission queue: %d\n", index_to_remove)
		tcbEntry.RetransmissionQueue = tcbEntry.RetransmissionQueue[index_to_remove+1:]
		tcbEntry.RetransmitCounter = 0 // reset retransmission counter
	}
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

	log.Printf("retransmit counter: %d\n", tcbEntry.RetransmitCounter)
	tcbEntry.RTOTimeoutChan <- true

	log.Print("reached end of update timeout")
}

// calculate SRTT function (SRTT should be equal to RTT on first packet sent)
func calculateSRTT(SRTTlast time.Duration, RTT int64) time.Duration {
	return time.Duration(int64((SRTTlast.Seconds()*ALPHA)+(float64(RTT)*(1-ALPHA))) * int64(time.Second))
}

// calculate RTO function (RTO should be equal to the RTO upper bound before the first packet is sent)
func calculateRTO(SRTT time.Duration) time.Duration {
	return time.Duration(int64(Min64f(RTO_UPPER.Seconds(), Max64f(BETA*SRTT.Seconds(), RTO_LOWER.Seconds()))) * int64(time.Second))
}

// goroutine function to check timeout
// TODO: where should we first call this routine?
func (t *TCPHandler) waitTimeout(tcbEntry *TCB, socketData *SocketData) {
	timeout := time.After(tcbEntry.RTO)
	for {
		select {
		case <-tcbEntry.RTOTimeoutChan:
			log.Print("resetting rto timeout")
			// resetting the timeout in this case, either we have received an ACK or we have sent a new segment
			timeout = time.After(tcbEntry.RTO)
		case <-timeout:
			// use the first element from the retransmission queue and send it to the IP layer
			// the element should only be removed once we have received an ACK for it
			// reset the timer when sending

			// only timeout if we have sent something, which means the retransmission queue is non-empty
			tcbEntry.TCBLock.Lock()
			if tcbEntry.State == TIME_WAIT {
				log.Print("exiting timeout due to time wait reached")
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
				// TODO: go directly into CLOSED?
				// what should be signaled in this case?
				log.Print("reached max retransmissions for data")
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

			log.Print("retransmitting data packet")
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

			tcbEntry.RTO *= 2 // exponential backoff
			log.Printf("reset value: %d\n", tcbEntry.RTO)

			timeout = time.After(tcbEntry.RTO) // reset the timeout

			// unlock mutex for TCB
			tcbEntry.TCBLock.Unlock()

			// extend the buffer that we are sending with the payload
			bufToSend = append(bufToSend, payload...)
			t.IPLayerChannel <- bufToSend
			log.Print("returning after sending to ip layer")
		}
	}
}

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

			log.Print("retransmitting syn ack")
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
