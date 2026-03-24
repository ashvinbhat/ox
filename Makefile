.PHONY: build install clean test

BINARY=ox
BUILD_DIR=./cmd/ox

build:
	go build -o $(BINARY) $(BUILD_DIR)

install:
	go install $(BUILD_DIR)

clean:
	rm -f $(BINARY)
	go clean

test:
	go test ./...

run:
	go run $(BUILD_DIR) $(ARGS)
