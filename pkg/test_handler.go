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
	fmt.Println()
	fmt.Fprint(t.w, "---Node received packet!---\n")
	fmt.Fprintf(t.w, "source IP: \t %s\n", packet.Header.Src.String())
	fmt.Fprintf(t.w, "destination IP: \t %s\n", packet.Header.Dst.String())
	fmt.Fprintf(t.w, "protocol: \t %d\n", packet.Header.Protocol)
	fmt.Fprintf(t.w, "payload length: \t %d\n", len(string(packet.Data[:])))
	fmt.Fprintf(t.w, "payload: \t %s\n", string(packet.Data[:]))
	fmt.Fprintf(t.w, "---------------------------\n")
	t.w.Flush()

}

func (t *TestHandler) InitHandler(data []interface{}) {
	t.w = new(tabwriter.Writer)
	t.w.Init(os.Stdout, 16, 10, 4, ' ', 0)
	return
}

func (t *TestHandler) AddChanRoutine() {
	return
}

func (t *TestHandler) RemoveChanRoutine() {
	return
}
