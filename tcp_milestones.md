# Milestone 1


- **What does a “SYN” packet or a “FIN” packet do to the receiving socket (in general)?**



- **What data structures/state variables would you need to represent each TCP socket?**
    - a socket object that is returned to the node
      - this can be of type VTCPListener or VTCPConn 
    - TCB object that would store the state for the socket 

- **How will you map incoming packets to sockets?**
  - We keep a socket table of (src ip, src port, dest ip, dest port) in our TCP handler.
  - when a host receives an IP packet with TCP protocol number, it will send the packet to the TCP handler which will check it against our socket table to find the correct socket object to send the packet to

- **What types of events do you need to consider that would affect each socket?**
    - Timeouts, out of sequence packets, sliding window
    - Receiving a FIN 

- **How will you implement retransmissions?**
    - some sort of the timeout and then resubmitting on timeout 

- **In what circumstances would a socket allocation be deleted? What could be hindering when doing so? Note that the state CLOSED would not be equivalent as being deleted.**
  - A socket allocation is deleted when both sides of the connection are CLOSED.
  - According to the RFC, there are 3 cases in which this finalizing state could be reached