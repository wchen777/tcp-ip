package tcp

import (
	"errors"
	"log"
)

func (t *TCPHandler) SetCCForSocket(vc *VTCPConn) error {
	log.Print("updating the tcb to have tahoe")
	var tcbEntry *TCB

	if val, exists := t.SocketTable[vc.SocketTableKey]; !exists {
		return errors.New("Socket doesn't exist")
	} else {
		tcbEntry = val
	}
	tcbEntry.CControlEnabled.Store(true)
	tcbEntry.TCBLock.Lock()
	defer tcbEntry.TCBLock.Unlock()

	// set the initial values for slow start, in which
	// init_cwnd is MSS
	tcbEntry.CWND = INIT_CWND
	// threshold is the largest advertised window
	tcbEntry.SSTHRESH = INIT_THRESHOLD

	return nil
}

// This should be called every time we have received a new ACK for data
func (t *TCPHandler) UpdateCWNDWindow(tcbEntry *TCB) {
	// tcbEntry should be locked on entry to this function
	if tcbEntry.CWND < tcbEntry.SSTHRESH {
		// increase the congestion window by MSS_DATA
		// this strategy might result in the window not properly doubling,
		// but asked on Ed and it said it was okay
		tcbEntry.CWND += MSS_DATA
		log.Printf("Updating the congestion window during slow start: %d\n", tcbEntry.CWND)
	} else {
		// congestion avoidance
		tcbEntry.CWND += (MSS_DATA * MSS_DATA) / tcbEntry.CWND
		log.Printf("Updating the congestion window during congestion avoidance: %d\n", tcbEntry.CWND)
	}
}

// This should be called every time there's been a loss:
// 		- ACK has been duplicated three times
//      - Reached the timeout case
func (t *TCPHandler) AfterLoss(tcbEntry *TCB) {
	log.Print("Reached a point of loss!")
	// assume that the lock is locked on entry
	// set the threshold to be the current window size divded by two
	tcbEntry.SSTHRESH = tcbEntry.CWND / 2
	// restart the congestion control, so we should start with slow start again
	tcbEntry.CWND = INIT_CWND
	log.Printf("New slow start threshold: %d\n", tcbEntry.SSTHRESH)
	log.Printf("New congestion window: %d\n", tcbEntry.CWND)
}
