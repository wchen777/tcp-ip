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