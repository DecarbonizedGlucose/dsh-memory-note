.PHONY: build test check

build:
	mkdir -p bin
	go build -o bin/dsh-memory-note ./cmd/dsh-memory-note

test:
	go test ./...

check:
	gofmt -w cmd internal test
	go vet ./...
	go test ./...
