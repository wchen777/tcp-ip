package pkg

type Host struct {
	HostName string

	HostComm HostLinkComm
	LinkIFs  []LinkInterface
	IPAddr   string
}
