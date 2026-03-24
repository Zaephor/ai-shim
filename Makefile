.PHONY: build test lint clean setup fmt vet e2e verify

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

fmt:
	gofmt -w .

vet:
	go vet ./...

e2e:
	go test ./test/e2e/ -v -count=1

verify: fmt vet lint test

setup:
	git config core.hooksPath .githooks
