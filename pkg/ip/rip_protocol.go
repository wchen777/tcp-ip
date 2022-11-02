package ip

type RIPEntry struct {
	Cost    uint32
	Address uint32
	Mask    uint32
}

type RIPMessage struct {
	Command    uint16
	NumEntries uint16
	Entries    []RIPEntry
}
