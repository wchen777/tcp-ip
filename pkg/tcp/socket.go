package tcp

type SocketType int
type ConnectionState int

const (
	LISTENER SocketType = iota
	CONNECTION
)

const (
	CLOSED ConnectionState = iota
	LISTEN
	SYN_SENT
	SYN_RECEIVED
	ESTABLISHED
	FIN_WAIT_1
	FIN_WAIT_2
	CLOSING
	TIME_WAIT
	CLOSE_WAIT
	LAST_ACK
)

type Socket interface {
	GetType() SocketType
}
