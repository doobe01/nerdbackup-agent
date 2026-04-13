BINARY=nerdbackup-agent
TRAY_BINARY=nerdbackup-tray
VERSION?=0.1.0
LDFLAGS=-ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build build-tray run clean test lint fmt vet coverage release install

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/nerdbackup-agent

build-tray:
	go build $(LDFLAGS) -o $(TRAY_BINARY) ./cmd/nerdbackup-tray

run: build
	./$(BINARY) run

clean:
	rm -f $(BINARY) $(TRAY_BINARY) coverage.txt
	rm -rf dist/

test:
	go test -v -race ./...

coverage:
	go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...
	go tool cover -func=coverage.txt

lint:
	golangci-lint run ./...

fmt:
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)

vet:
	go vet ./...

install: build
	cp $(BINARY) /usr/local/bin/$(BINARY)

release:
	goreleaser release --clean

check: fmt vet lint test
	@echo "All checks passed"
