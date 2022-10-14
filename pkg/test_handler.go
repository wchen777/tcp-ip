package pkg

import "fmt"

type TestHandler struct {
	ProtocolNumber int
}

func (t *TestHandler) ReceivePacket(packet IPPacket, data []interface{}) error {
	fmt.Print("Received packet data in test handler...")
	fmt.Print(packet.Data)
	return nil
}
