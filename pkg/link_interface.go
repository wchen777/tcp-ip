package pkg

type LinkInterface struct {
	InterfaceNumber int
	HostIPAddress   string
	// DestAddress, TODO: does the interface know about the destination? Or is it something that's learned?
	IPAddress string
}
