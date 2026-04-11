.PHONY: test build

test:
	go test ./...

build:
	mkdir -p bin
	go build -o bin/agentctl ./cmd/agentctl
