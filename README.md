# TCP 

## API Design Choices
### What happens when a packet is received 
-- Floria 

### VTCPConn
-- Floria 

**Read**

**Write**


### VTCPListener
-- Floria 

## Sender
-- Floria 

### Write call

## Receiver

Upon arrival of a segment, we split up each different TCP state with each of their own handlers to process the packet differently, depending on what packets that state expects.

In the handshake, when a packet arrives that conforms to the expectations in the RFC (for example when an ACK arrives for a SYN in `SYN-SENT`), we progress to the next state and create TCB data structures when appropriate.

When in `ESTABLISHED`, we utilize a function called `Receive()` in `tcp_handler_core.go` that processes incoming packets. (This function is also used to process packets normally in states such as `FIN WAIT 2`).

This function checks to see if the packet has a payload, if so, it checks to see whether the packet's sequence number is valid, adds the payload to the buffer if so, and increments necessary buffer pointers. 
Then it will send an ACK back to the sender acknowledging what it currently has if the packet's sequence number was invalid, or the updated pointer values if the receiver was able to process data. This process also signals waiting threads blocked on a condition that there needs to be data to be received in the buffer before reading.  

Because the receiver can also send data, we also check the ACK number this packet has. If the ACK number is valid and acknowledges new data, then we increment necessary pointers for the sender side. This also signals blocked threads waiting on more data to be acknowledged in the buffer before sending more, as well as if the window has now been updated from 0 to stop zero-probing.

### Early Arrivals 
We used a min-heap data structure sorted based on the sequence number of the packet as our early arrivals queue. This queue stored the payload length as well which allowed us to find the "boundary" of what segment this packet contained in the buffer.

When a packet received that had a sequence number that was greater than the receiver's NXT pointer but still within the window, we would add it to our early arrivals data structure.

When a packet received has the expected sequence number of equal to our receiver's NXT pointer, we checked to see the early arrivals queue if any packets in the queue could be "merged" with this packet and ACK'd all at once. If so, we would return the highest ACK num that the receiver could send back that acknowledged a continuous segment in the buffer, updating all necessary pointers accordingly.



## Connection Teardown 
-- Floria 

## Handling Timeout/Retransmissions
To implement timeouts and retransmissions, we used a timer for each TCB entry/socket that would reset once a packet is sent or if an ACK was received that acknowledged new data that was sent. 

If the timer were to timeout, we would send the most recently unacknowledged packet in a retransmission queue, which contained the payload length and the packet's header. We chose to store these values as we could use the header data to reconstruct the packet, and find the payload data by re-indexing into the buffer using the payload length and the packets sequence number in the header. We also have the guarantee that the retransmission queue is always ordered based on sequence number due to the sender always sending sequentially (and the data is also guaranteed to be in the buffer).
When an ACK would be received, we would need to update the retransmission queue by removing all packets that the incoming ACK num had acknowledged.

To calculate the dynamically updating timeout values, we used the RTO (retransmission timout) formula given in lecture that utilized the previous SRTT (smoothed round-trip time) and the current calculated RTT (round-trip time) in order to calculate the new SRTT.
We did this by storing a map from a packet's expected ACK number (the sequence number + length of payload) to a timestamp, and when the ACK would be received for that packet, we took the difference in timestamps and found the RTT, which would be used to find the RTO. On the first packet, we special cased the SRTT to just be the RTT.


## Congestion Control 
-- Floria 


--- 

## Measuring Performance

Throughput graph of a 1MB sendfile through with a 2% lossy node in between:

![throughput](packet_capture/throughput "throughput")

## Packet Capture

(see `packet_capture/1mb_over_lossy_middle.pcapng` for the packet capture)

For a 1MB sendfile through with a 2% lossy node in between:

### Handshake

Packets 27, 28, 29.

![handshake](packet_capture/handshake.png "Handshake")

Connection `192.168.0.4` is initiating the connection. It sends a SYN (packet 27), which is ACK'd by the receiver, who sends its own SYN (packet 28) which is then ACK'd by the sender (packet 29).

### One example segment sent and acknowledged

Packets 56, 62.

![sent+ack](packet_capture/sent+ack.png "Send and Ack")

56: `192.168.0.4` sends a packet with sequence number 16385 and payload length 1360, expecting an ACK number of 16385+1360= 17745
57-61: (`192.168.0.4` continues to send through its buffer) 
62: `192.168.0.1` responds with an ACK acknowledging (all data up until) this packet.

### One segment that is retransmitted

Packets 185 (not show in screenshot), 235, 236.

![retransmission](packet_capture/retransmission.png "Retransmission")

185: `192.168.0.4` sends a packet with sequence number 103617.
233,234: `192.168.0.1` still has not received a packet with sequence number 103617 although `192.168.0.4` has sent beyond this sequence number. `192.168.0.1` responds that it is still expecting 103617
235: `192.168.0.4` times out and retransmits packet with sequence number 103617.
236: `192.168.0.1` is able to acknowledge this packet, along with the other packets it has queued up in early arrivals, responding with sequence number 131072

### Connection Teardown

Packets: 2270, 2274, 2275, 2276

![teardown](packet_capture/teardown.png "Teardown")

2270: `192.168.0.4` is done sending, so it sends a FIN
2271-2273: `192.168.0.1` is not done acknowledging data, so it queues the FIN and responds with its current ACK number, while acknowledging more data
2274: `192.168.0.1` acknowledges the FIN sent by `192.168.0.4`
2275: `192.168.0.1` now can send its own FIN to terminate the connection
2276: `192.168.0.4` acknowledges the FIN sent by `192.168.0.4`


--- 

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
  - cleanup any error checking/handling 
  - send a rip request without any entries on startup (handle rip request/rip response)