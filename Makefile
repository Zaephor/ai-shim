.PHONY: build test lint clean setup

BINARY := ai-shim
MODULE := github.com/ai-shim/ai-shim

build:
	go build -o $(BINARY) ./cmd/ai-shim

test:
	go test ./... -v

test-short:
	go test ./... -v -short

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)
	go clean -testcache

setup:
	git config core.hooksPath .githooks
