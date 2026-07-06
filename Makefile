.PHONY: build install clean test

BINARY=ox
BUILD_DIR=./cmd/ox
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS=-X github.com/ashvinbhat/ox/internal/version.Version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) $(BUILD_DIR)

install:
	go install -ldflags "$(LDFLAGS)" $(BUILD_DIR)

clean:
	rm -f $(BINARY)
	go clean

test:
	go test ./...

run:
	go run $(BUILD_DIR) $(ARGS)
