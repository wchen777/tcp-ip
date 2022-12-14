package tcp

import "go.uber.org/atomic"

// Storing a socket data key so that we can index into the socket table
// in the handler
type VTCPListener struct {
	SocketTableKey SocketData
	TCPHandler     *TCPHandler
	Cancelled      *atomic.Bool
}

func (vl *VTCPListener) GetType() SocketType {
	return LISTENER
}

func (vl *VTCPListener) GetSocketTableKey() SocketData {
	return vl.SocketTableKey
}

/*
 * Waits for new TCP connections on this listen socket.  If no new
 * clients have connected, this function MUST block until a new
 * connection occurs.

 * Returns a new VTCPConn for the new connection, non-nil error on failure.
 */
func (vl *VTCPListener) VAccept() (*VTCPConn, error) {
	// use the tcp handler in the listener struct to cleanup from the table
	return vl.TCPHandler.Accept(vl)
}

/*
 * Closes the listening socket, removing it
 * from the socket table.  No new connections may be made
 * on this socket--any pending requests to establish
 * connections on this listen socket are deleted.
 */
func (vl *VTCPListener) VClose() error {
	// use the tcp handler in the listener struct to cleanup from the table
	return vl.TCPHandler.CloseListener(vl)
}
