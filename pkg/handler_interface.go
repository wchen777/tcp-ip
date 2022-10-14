package pkg

import "ip/pkg"

type Handler interface {
	ReceivePacket(pkg.IPPacket, []interface{}) error
}
