package tcp

import "go.uber.org/atomic"

// Storing a socket data key so that we can index into the socket table
// in the handler
type VTCPConn struct {
	SocketTableKey SocketData
	TCPHandler     *TCPHandler
	ReadCancelled  *atomic.Bool // TODO: maybe these fields can be set when shutdown/closed are called so we know which API calls cannot be made
	WriteCancelled *atomic.Bool
}

func (vc *VTCPConn) GetType() SocketType {
	return CONNECTION
}

func (vc *VTCPConn) GetSocketTableKey() SocketData {
	return vc.SocketTableKey
}

/*
  * Reads data from the TCP connection (RECEIVE in RFC)
  * Data is read into slice passed as argument.
  * VRead MUST block when there is no available data.  All reads should
  * return at least one byte unless failure or EOF occurs.
  * Returns the number of bytes read into the buffer.  Returned error
  * is nil on success, io.EOF if other side of connection was done
  sending, or other error describing other failure cases.
*/
func (vc *VTCPConn) VRead(data []byte, amountToRead uint32, readAll bool) (uint32, error) {
	return vc.TCPHandler.Read(data, amountToRead, readAll, vc)
}

/*
 * Write data to the TCP connection (SEND in RFC)
 *
 * Data written from byte slice passed as argument.  This function
 * MUST block until all bytes are in the send buffer.
 * Returns number of bytes written to the connection, error if socket
 * is closed or on other failures.
 */
func (vc *VTCPConn) VWrite(data []byte) (uint32, error) {
	return vc.TCPHandler.Write(data, vc)
}

/*
 * Shut down the connection
 *  - If type is 1, close the writing part of the socket (CLOSE in
 *    RFC).   This should send a FIN, and all subsequent writes to
 *    this socket return an error.  Any data not yet ACKed should
 *    still be retransmitted.
 *  - If type is 2, close the reading part of the socket (no RFC
 *    equivalent); all further reads on this socket should return 0,
 *    and the advertised window size should not increase any further.
 *  - If type is 3, do both.
 * Reuturns nil on success, error if socket already shutdown or for
 * other failures.
 *
 * NOTE:  When a socket is shut down, it is NOT immediately
 * invalidated--that is, it remains in the socket table until
 * it reaches the CLOSED state.
 */
func (vc *VTCPConn) VShutdown(sdType int) error {
	return vc.TCPHandler.Shutdown(sdType, vc)
}

/*
 * Invalidate this socket, making the underlying connection
 * inaccessible to ANY of these API functions.  If the writing part of
 * the socket has not been shutdown yet (ie, CLOSE in the RFC), then do
 * so.  Note that the connection shouldn't be terminated, and the socket
 * should not be removed from the socket table, until the connection
 * reaches the CLOSED state.  For example, after VClose() any data not yet ACKed should still be retransmitted.
 */
func (vc *VTCPConn) VClose() error {
	return vc.TCPHandler.Close(&vc.SocketTableKey, vc)
}
