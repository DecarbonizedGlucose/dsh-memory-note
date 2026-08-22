.PHONY: build test check adapter-check adapter-test check-all

build:
	mkdir -p bin
	go build -o bin/dsh-memory-note ./cmd/dsh-memory-note

test:
	go test ./...

check:
	gofmt -w cmd internal test
	go vet ./...
	go test ./...

adapter-check:
	cd adapter && pnpm typecheck

adapter-test:
	cd adapter && pnpm test

check-all: check adapter-check adapter-test
