.PHONY: build test lint clean setup fmt vet e2e e2e-ci verify check-silent-failures

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

e2e-ci:
	AI_SHIM_CI=1 go test ./test/... ./internal/container/ ./internal/dind/ ./internal/network/ ./internal/docker/ -v -count=1 -timeout 600s

check-silent-failures:
	@echo "Checking for silent failure patterns in production code..."
	@if grep -rn '2>/dev/null' internal/ --include='*.go' | grep -v '_test.go' | grep -v 'command -v'; then \
		echo "ERROR: Found 2>/dev/null in production code (silent failure pattern)"; \
		exit 1; \
	fi
	@echo "No silent failure patterns found."

verify: fmt vet lint test check-silent-failures

setup:
	@command -v lefthook >/dev/null || (echo "Installing lefthook..." && go install github.com/evilmartians/lefthook@latest)
	lefthook install
