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

func SocketStateToString(state ConnectionState) string {
	switch state {
	case CLOSED:
		return "CLOSED"
	case LISTEN:
		return "LISTEN"
	case SYN_SENT:
		return "SYN_SENT"
	case SYN_RECEIVED:
		return "SYN_RECEIVED"
	case ESTABLISHED:
		return "ESTABLISHED"
	case FIN_WAIT_1:
		return "FIN_WAIT_1"
	case FIN_WAIT_2:
		return "FIN_WAIT_2"
	case TIME_WAIT:
		return "TIME_WAIT"
	default:
		return "LAST_ACK"
	}
}

type Socket interface {
	GetType() SocketType
	GetSocketTableKey() SocketData
}
