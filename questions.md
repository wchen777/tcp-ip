<!-- - closed vs deleted? Does deleted mean that both ends are closed?  -->
<!-- - If the listen was not fully specified (i.e., the remote socket was not fully specified), then the unspecified fields should be filled in now.
- but this is not appropriate when the stack is capable of sending data on the SYN because the TCP peer may not accept and acknowledge all of the data on the SYN.
- scenario in which a simultaneous open happens? how can two sockets both send syn's (both are CONNECT)
  - why would UNA be == to ISS in simultaneous open?

- SND.WL1 <- SEG.SEQ
- SND.WL2 <- SEG.ACK -->
<!-- queue them for processing after the ESTABLISHED state has been reached, return. -->

<!-- SYN-RECEIVED STATE
If SND.UNA < SEG.ACK =< SND.NXT, then enter ESTABLISHED state  why do we not update the una? 

<!-- - how to maintain a listener socket in our socket table once a new connection is established?
  - do we create a new tcb with the new state variables and reset the listener to be in listen state and reset variables as well? Should the listener be the one handling the handshake?  -->

<!-- - tcp socket accept+connect to itself? -->

Window vs. buffer terminology
- When the indices within the buffer wrap around, how do we keep track of it's distance relative to the (initial) sequence number? 
  - do we need to keep track of how many times the indices wrapped around?
  - UNA, NXT should be in terms of sequence number of buffer indicies? For example if you have UNA at 20, does that mean it's 20+ISS or 20+(MAX_BUF*n)+ISS? 
  - Or should we keep track of where the bounds of the buffer are relative to the ISS? But how do we handle wrapping around? Or are the bounds of the buffer shifting rather than wrapping around?

### TODOS: 

**tcp handler:**
- synchronization for socket table? + access to entries?




