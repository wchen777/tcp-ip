- How big can an IP packet get? If it's over 1400 bytes, should we split it into multiple IP packets and send them over the link layer? How can we tell when to stop if we do split it into different frames? 

- Understanding interfaces -- interface IP address vs. host IP address, sending packets through interfaces (does A's interface have to know about B's interface), is the connection between interfaces learned?

- enabling/disabling the interface 
- each interface has its own ip address 

- each host has only one UDP port 
    - all interfaces of that host will receive from that port




where to initialize UDP socket when sending messages through link layer

registering application layer handler and why we need this -- HandlerFunc, still differentiating between protocol numbers?, write a generic function that differentiates between the two handlers somehow?



- closed vs deleted? Does deleted mean that both ends are closed? 
- how do we choose the initial port number when connecting? 
- If the listen was not fully specified (i.e., the remote socket was not fully specified), then the unspecified fields should be filled in now.
- but this is not appropriate when the stack is capable of sending data on the SYN because the TCP peer may not accept and acknowledge all of the data on the SYN.
- scenario in which a simultaneous open happens? how can two sockets both send syn's (both are CONNECT)
  - why would UNA be == to ISS in simultaneous open?

- SND.WL1 <- SEG.SEQ
- SND.WL2 <- SEG.ACK

queue them for processing after the ESTABLISHED state has been reached, return.

SYN-RECEIVED STATE
If SND.UNA < SEG.ACK =< SND.NXT, then enter ESTABLISHED state --> why do we not update the una? 

- how to maintain a listener socket in our socket table once a new connection is established?
  - do we create a new tcb with the new state variables and reset the listener to be in listen state and reset variables as well? Should the listener be the one handling the handshake? 



### TODOS: 

**Node:**
- initialize channels (tcp channel) + other variables required for tcp handler

**tcp handler:**
- synchronization for socket table? + access to entries?




