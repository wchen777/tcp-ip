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

Our problem:
- UNA and NXT are in terms of buffer (for send)
  - i.e. buffer in sequence number address space starts at ISS, but UNA starts at 0 
- we keep sending and writing data, such that UNA and NXT are incremented to a value greater than MAXBUF
- UNA and NXT wrap to 0
- need a way to keep track of UNA and NXT relative to the sequence number space
- the reason we have this is so that when the application writes to the buffer, we are able to wrap around the end of the buffer until UNA to determine when the application is able to write 

Nick's ideas:
- Two ideas
  - keep track of two versions of UNA (and NXT) alongside eachother, uint16 UNA_IND (for the buffer) and uint32 UNA_SEQ (sequence space)
    - always update both at the same time, so that when we want to index into the buffer, we can use UNA_IND, or if we want to send out an ACK, use UNA_SEQ
  - keep track of 1 uint32 UNA
    - whenever we want to index into the buffer, mod by 2^16, and use that value as our buffer index
    - this handles the wrap-arounds for the buffer
    - send this UNA as absolute SEQ number

- if N is specified as an option, does that mean calling read should never block? 


### TODOS: 

**tcp handler:**
- synchronization for socket table? + access to entries?
- refactor the ACK sending to own function, same thing with the packet sending? (do this later)

**features to implement**
- checksum 
- handshake timeout
- sendfile command 
- retransmission queue and timeout

**error conditions to handle**
- are we handling when we send to/read froma a socket that doesn't exist?
- calling read on a listener socket 
- calling accept on a non-listener socket 
- basically what should happen when the operation isn't supported on a socket 

- is shutdown type 3 just close? 

- send / rec lock vs TCB lock
  - difficulty is that the sender might need to touch the receive TCB for values, which may cause deadlock if we take a lock on the receiver while they're trying to take a lock for us
  - cond's depending on the send/rec lock ^^ rather than TCB lock
  - right now use TCB lock for multiple condition variables, not sure if this is good
  - NICK SAYS:: use atomic integers, don't need to "cross lock"
  - if, as a sender, the RCV.NXT is old -> this is ok
  - if, as a sender, the RCV.WND is old (bigger than what it should be) -> need to check for a payload that extends farther than our window and truncate
    - we got rid of this check, so maybe add it back?
  - (check as the other cases where as a receiver, the sender fields are older due to not taking a lock on the SND values)

- when should a receiver ACK?
  - should ACK **any** packet that has data (len(payload) > 0), but with our current RCV.NXT.
    - even if the packet falls out of range of our window, or is less than NXT, ACK back with what we have and do not process the data
    - if the packet does not have data, the receiver does not need to process it (or send an ACK back)
  - if it doesn't have data, it may have been meant for the sender side, so still process it as well

- data structures for early arrivals queue
  - FIFO queue:
  - buffered channel, set a reasonable maximum (but how would we manage segment numbers)
  - **min heap???** -> just store the (sequence number, seq num + payload size) received
  - segments received early, what we just need is the boundaries for which the segments were received, (metadata to tell which is the indexing)
  - the actual data itself can be copied into the received buffer? 
    - if it doesn't fit, throw it away

- worried about cond's with separate locks,
  - ex. how will we ensure that the signaller has the mutex that is associated with the cond

