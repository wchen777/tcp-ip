# Milestone 1


- **What does a “SYN” packet or a “FIN” packet do to the receiving socket (in general)?**
  - SYN: initiates a connection (generally), moves the socket state from CLOSED to SYN_RECEIVED, if a socket was in SYN_SENT and received a SYN, it would also go to SYN_RECEIVED
  - FIN: tells the receiving socket that the sender has initiated a close, moves the receiving socket into a passive close state (CLOSE WAIT). after the initial sender of the FIN packet receives its own FIN from the other side, it also enter the TIME WAIT state.


- **What data structures/state variables would you need to represent each TCP socket?**
    - a socket object that is returned to the node
      - this can be of type VTCPListener or VTCPConn 
    - TCB object that would store the state for the socket 

- **How will you map incoming packets to sockets?**
  - We keep a socket table of (src ip, src port, dest ip, dest port) in our TCP handler.
  - when a host receives an IP packet with TCP protocol number, it will send the packet to the TCP handler which will check it against our socket table to find the correct socket object/TCB to send the packet to
  - 

- **What types of events do you need to consider that would affect each socket?**
    - Timeouts, out of sequence packets, sliding window
    - Receiving a FIN/SYN

- **How will you implement retransmissions?**
    - some sort of the timeout and then resubmitting on timeout 

- **In what circumstances would a socket allocation be deleted? What could be hindering when doing so? Note that the state CLOSED would not be equivalent as being deleted.**
  - A socket allocation is deleted when both sides of the connection are CLOSED, this is the "finalizing state" as mentioned in the RFC.
  - According to the RFC, there are 3 cases in which this finalizing state could be reached
    - the TCB/socket allocation is deleted upon a LISTEN'er socket being closed from a user's calling of the close function
    - after a connection is established, a FIN is received in which the receiving socket moves to a passive close state while the FIN sending socket moves to active close state, after either connection wait until their FIN's have been ACK'd are the sockets fully closed and deleted
    - both sockets attempt to send FIN packets (simultaneous close), both sockets must also wait until their FIN's are ACK'd are the connections deleted
