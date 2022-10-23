package traceroute

// general approach:
// send a packet with TTL 1 --> when does TTL get decremented?
// define some protocol number

type TimeExceededMessage struct {
}
