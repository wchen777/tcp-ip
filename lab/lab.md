## Tasks

Look at the headers on both packets–what fields changed as the packet was forwarded?

- TTL -> 16 to 15
- Checksum -> 0x2982 to 0x2a82

### RIP Dissecting

1. What node sent this RIP update? To which node was it sent?
  - Node B -> Node C
2. What entries are sent in the update? Do you observe instances of split horizon/poison reverse?
  - Both of Node B's interfaces with cost 0
  - Node A with cost 1
  - Node C, poisoned with cost 16 (Node B poisons the response to Node C for all entries it learned from Node C)
