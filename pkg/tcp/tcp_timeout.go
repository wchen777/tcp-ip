package tcp

import "time"

// need retransmission queue (TCB level)
// keep track of how many times we have retransmitted

// need to reset retransmit timer on packet send and retransmit packet send

const (
	ALPHA     = 0.85
	RTO_UPPER = time.Duration(time.Second * 60)
	RTO_LOWER = time.Duration(time.Second)
	BETA      = 1.5
)

// on packet receive, used the updated
func (t *TCPHandler) updateTimeout(tcbEntry *TCB, segmentReceivedNum uint32) {
	// assume TCB is locked here, and will exit with it locked.

	RTT := time.Now().Unix() - tcbEntry.SegmentToTimestamp[segmentReceivedNum] // round-trip time

	SRTT := calculateSRTT(tcbEntry.SRTT, RTT) // smoothed round-trip time
	tcbEntry.SRTT = SRTT

	RTO := calculateRTO(SRTT) // retransmission timeout (in time duration)
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
func (t *TCPHandler) waitTimeout(tcbEntry *TCB) {
	timeout := time.After(time.Duration(tcbEntry.RTO * time.Second))
	for {
		select {
		case <-tcbEntry.RTOTimeoutChan:
			// resetting the timeout in this case
			timeout = time.After(time.Duration(tcbEntry.RTO * time.Second))
		case <-timeout:
			//
		}
	}
}
