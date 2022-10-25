package pkg

type Handler interface {
	ReceivePacket(packet IPPacket, data interface{})
	InitHandler(data []interface{})
	AddChanRoutine()
	RemoveChanRoutine()
}
