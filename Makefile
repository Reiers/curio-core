BIN=bin/curiocore

.PHONY: build test fmt

build:
	mkdir -p bin
	go build -o $(BIN) ./cmd/curiocore

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal
