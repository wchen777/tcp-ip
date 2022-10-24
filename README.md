# IP

## Abstraction between link layer and its interfaces
- We created a struct that represents the link interface, which contains information about the UDP port and address, the interface number, the interface's ip address, and whether the interface is up/down 
- Created a host struct that represents the IP layer, which is responsible for determining where to forward ip packets and interfacing with the different handlers that may receive those packets of data 
- Each handler had its own struct that implemented a handler interface struct, on startup, the host will register the individual handlers 

## Threads and RIP protocol 

### Threads

These goroutines are called in `StartHost`:
- Go routine that is listening for packets on the host through the UDP protocol
- Go routine on the host that is listening for to-be-sent messages from the RIP handler, which is then sent through the correct interface

### RIP Protocol



## Steps on receiving an IP Packet 
Upon receiving an IP packet from an interface on the host's UDP port, we:
- check the IP version, if it is not IPV4, we drop the packet
- we recompute the checksum and compare it against the previous value 
- decrement TTL
- check the destination:
  - if it is meant for us (it has reached its destination):
    - we handle the packet in the respective application handler with the protocol number
  - otherwise we need to forward the packet:
    - check if the TTL is == 0, if it is then the packet should be dropped
    - if we reached this point, we are good to send consult the routing table for the correct next hop and process the packet on the correct interface

## Known bugs 
We hope none at the moment.

## Design Decisions 

We chose to have one UDP connection per host, and then reused this connection for all the interfaces when sending to destinations as well as listening for incoming messages. The host would have one goroutine to listen on the interface for incoming messages.

When registering handlers, we have an generic `InitHandler()` function that takes in generic data to set up the respective handler. For RIP, this means that we need to pass in the routing table, the host's interfaces, and the host's neighbors so that the RIP handler would have access to this information.


For communication between the different layers, we added a couple different channels. 
The has a channel to: 
- receive IP packets from the link layer. This is so that the link interface doesn't necessarily need to know about the host that it's connected to -- i.e. having more of hirearchial relationship between the host and its link interfaces. When passing data to send via the link layer, the host will invoke the Send function of the local interface and pass the IP packet to send. 
 
- receive bytes of data from the rip handler to create an IP packet to send to the link layer. This channel will be used when the RIP protocol periodically sends updates every five seconds or for triggered updates. The host will propogate data received to the RIP handler by passing it to the ReceivePacket routine that is part of each protocol handler. 

## Traceroute 


## To run
We developed this project in the container environment. 

To run, you can run `make` or `make clean all` which should create an executable node, which takes in a lnx file as an argument. 


## TODO: 
- code cleanup! in `host.go` 
  - cleanup "link layer reading" abstraction
  - consolidate checksum and sending packet stuff
  - cleanup any TODOs in code base 
  - cleanup any error checking/handling 
  - send a rip request without any entries on startup (handle rip request/rip response)