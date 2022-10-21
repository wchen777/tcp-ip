package pkg

import (
	"fmt"
	"os"
	"text/tabwriter"
)

/*
	---Node received packet!---
        source IP      : <source VIP>
        destination IP : <destination VIP>
        protocol       : 0
        payload length : <len>
        payload        : <msg>
	---------------------------
**/

type TestHandler struct {
	w *tabwriter.Writer
}

func (t *TestHandler) ReceivePacket(packet IPPacket, data interface{}) {
	fmt.Println("---Node received packet!---")
	fmt.Fprintf(t.w, "source IP \t %s\n", packet.Header.Src.String())
	fmt.Fprintf(t.w, "destination IP \t %s\n", packet.Header.Dst.String())
	fmt.Fprintf(t.w, "protocol \t %d\n", packet.Header.Protocol)
	fmt.Fprintf(t.w, "payload length \t %d\n", packet.Header.Len)
	fmt.Fprintf(t.w, "payload \t %s\n", string(packet.Data[:]))
	fmt.Println("---------------------------")
	t.w.Flush()

}

func (t *TestHandler) InitHandler(data []interface{}) {
	t.w = new(tabwriter.Writer)
	t.w.Init(os.Stdout, 16, 10, 4, ':', 0)
	return
}
