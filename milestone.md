## Milestone 
- What objects will you use to abstract link layers, and what interface will it have?
    - Define an interface that implement the wrapper functions that would make the calls to the socket file descriptors. The "object" will store information such as the UDP socket and "physical" IP addresses/ports associated with it. 
    - We need an struct definition that represents the interface that packets are being sent and received from via the "physical" links, and it needs to know its own address, as well as the addresses of the links between nodes. For instance, in the example in the project handout, A's interface 0 needs to know about B's interface 0. 

- What fields in the IP packet are read to determine when to forward a packet?
    - IP version --> if the version is IPv6, the packet should be dropped
    - Header Checksum --> if the checksum is not valid, the packet should also be dropped 
    - TTL --> if the `TTL == 0`, then the packet should be dropped 
    - Destination address --> if it's the node itself, the packet doesn't need to be forwarded, and if the packet matches a network in the forwarding table, it should be sent on that interface. And if all the above conditions are false, then sent to next hop. And if the next hop doesn't exist, the packet is dropped. 

- What will you do with a packet destined for local delivery (ie, destination IP == your node’s IP)?
    - The packet will be sent to the application layer, which will be handled by the test handler and print out the packet contents.

- What structures will you use to store routing information?
    - We are planning on using a map in golang to map the IP prefix address to the interface, which will represented some struct that stores information relative to that interface. We're still thinking about where to store the IP address local to the host itself -- whether that should be a separate field or also stored in the forwarding table. 

- What happens on your node when the topology changes? In other words, when links go up down, how are forwarding and routing affected?
    - Forwarding: 
