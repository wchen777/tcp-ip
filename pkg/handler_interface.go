package pkg

//import "ip/pkg"

type Handler interface {
	ReceivePacket(IPPacket, interface{})
}
