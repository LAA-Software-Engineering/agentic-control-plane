.PHONY: test build

fmt:
	go fmt ./...

test:
	go test ./... -race

test-coverage:
	go test ./... -race -coverprofile=coverage.out

vet:
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/agentctl ./cmd/agentctl
