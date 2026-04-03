BINARY=nerdbackup-agent
VERSION?=0.1.0
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build run clean test

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/nerdbackup-agent

run: build
	./$(BINARY) run

clean:
	rm -f $(BINARY)
	rm -rf dist/

test:
	go test ./...

lint:
	golangci-lint run ./...
