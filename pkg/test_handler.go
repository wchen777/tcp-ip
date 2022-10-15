package pkg

import "fmt"

type TestHandler struct {
}

func (t *TestHandler) ReceivePacket(packet IPPacket, data interface{}) {
	fmt.Print("Received packet data in test handler...")
	fmt.Print(packet.Data)
}
