BIN=bin/curio

.PHONY: build test fmt

build:
	mkdir -p bin
	go build -o $(BIN) ./cmd/curio

test:
	go test ./...

fmt:
	gofmt -w ./cmd ./internal
