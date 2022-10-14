.PHONY: all clean

all: node

clean: 
	rm -f node

node: 
	go build ./cmd/node/
