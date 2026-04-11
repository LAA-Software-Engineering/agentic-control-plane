.PHONY: test build

test:
	go test ./... -race

test-coverage:
	go test ./... -race -coverprofile=coverage.out

build:
	mkdir -p bin
	go build -o bin/agentctl ./cmd/agentctl
