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

We also have a goroutine when we process a packet for a handler (`ReceivePacket`) (so that it doesn't block the calling thread).
For the same reason above, we run the function to forward a packet to be sent on the correct interface in a goroutine.

In the RIP handler, we have goroutines to handle timeouts. Whenever a new entry is created in the routing table, we create a new goroutine that is constantly waiting on a timer. This goroutine is associated with a channel for that entry that accepts an "update" message to reset the timer.

If the timer reaches the timeout before an update message (from the periodic updates) is received, the entry gets updated with a cost of INFINITY (effectively deleting the entry). 

And if the entry received from the RIP message already exists in the routing table, it is determined if it needs to be updated in the routing table, and if it needs to be updated, then the entry is updated to the routing table and added to a list. After iterating through all of the entries, a goroutine is created that will send all the updated entries to all the neighbors. And for each of these entries, it is checked if they are not in the host's own IP addresses, and if it's not, then an update is sent through the channel. Entries with a different next hop are ignored. 

Separately from all of this is a goroutine that is running asynchronously that will every five seconds, send an update to the neighbors and once a RIP message has been created, it will send that message through a channel to the host. 

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
We hope none at the moment. There's a bit of weirdness when running with the reference node and one of the node is quit, there's an entry is that might be printed with a cost of 17, which means that an entry with 16 is sent and it's updated when entries of 16 shouldn't be forwarded. 

## Design Decisions 
We chose to have one UDP connection per host, and then reused this connection for all the interfaces when sending to destinations as well as listening for incoming messages. The host would have one goroutine to listen on the interface for incoming messages.

When registering handlers, we have an generic `InitHandler()` function that takes in generic data to set up the respective handler. For RIP, this means that we need to pass in the routing table, the host's interfaces, and the host's neighbors so that the RIP handler would have access to this information.

For communication between the different layers, we added a couple different channels. 
The has a channel to: 
- receive IP packets from the link layer. This is so that the link interface doesn't necessarily need to know about the host that it's connected to -- i.e. having more of hirearchial relationship between the host and its link interfaces. When passing data to send via the link layer, the host will invoke the Send function of the local interface and pass the IP packet to send. 
 
- receive bytes of data from the rip handler to create an IP packet to send to the link layer. This channel will be used when the RIP protocol periodically sends updates every five seconds or for triggered updates. The host will propogate data received to the RIP handler by passing it to the ReceivePacket routine that is part of each protocol handler. 

## Traceroute 
Added two different ICMP messages: 
- Echo --> this is used to ping the hosts with incrementing TTLs until an echo reply is sent back indicating that the route has finished 
- Time exceeded --> this is used to let the thread running traceroute know the next hop in the sequence 
I added an additional handler that will process the different ICMP messages and handle them accordingly. Implemented by adding an additional handler, which would process the ICMP messages and handle them accordingly. 
    - Echo reply --> means that traceroute has found a destination and that information should be propogated to the host thread that is waiting for a response 
    - Echo --> means that we've reached the destination from a source that initiated traceroute and that from this host, a echo reply message shouldn't be sent back to the source 
    - Time limit exceeded message --> the host that initiated the traceroute found the next step in the route and can add that to the paths 

Added the traceroute to the CLI, which can be ran by invoking `traceroute dest` where dest is the IP address of the desired destination. This will invoke a routine in the host, and the host will initiate the protocol by sending echo packets of increasing TTL and after sending each packet will wait for a response. This is assuming that none of the packets will be dropped. Before sending the packets, the destination address is first checked if it's reachable, if it's unreachable, an empty list is returned. 

The handler also has an additional AddChanRoutine and RemoveChanRoutine, which is used such that in the case that a time limit exceeded message is received but it wasn't due to a traceroute command that the routine doesn't hang or block. 

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