.PHONY: build dev test lint clean setup fmt vet e2e e2e-ci verify check-silent-failures test-journey fmt-check test-race tidy-check fuzz vuln ci

BINARY := ai-shim
MODULE := github.com/Zaephor/ai-shim
VERSION ?= $(shell git describe --tags --dirty --always 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/ai-shim

# Dev build: no version ldflags — runtime/debug embeds the VCS commit hash
# automatically, producing "dev-<hash>" (or "dev-<hash>-dirty").
dev:
	go build -trimpath -o $(BINARY) ./cmd/ai-shim

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
	AI_SHIM_CI=1 go test ./test/... ./internal/container/ ./internal/dind/ ./internal/network/ ./internal/docker/ -v -count=1 -timeout 600s -race

check-silent-failures:
	@echo "Checking for silent failure patterns in production code..."
	@if grep -rn '2>/dev/null' internal/ --include='*.go' | grep -v '_test.go' | grep -v 'command -v'; then \
		echo "ERROR: Found 2>/dev/null in production code (silent failure pattern)"; \
		exit 1; \
	fi
	@echo "No silent failure patterns found."

test-journey:
	go test ./test/integration/ ./test/e2e/ -v -count=1 -run "Journey" -timeout 300s

verify: fmt vet lint test check-silent-failures

# --- CI mirror -------------------------------------------------------------
# These targets reproduce the GitHub Actions gate locally so a phase can be
# validated before it is considered done. `make ci` runs the non-Docker gate;
# round it out with `make lint` (golangci-lint) and `make e2e-ci` (Docker).

# Formatting check (non-mutating, unlike `fmt`).
fmt-check:
	@bad=$$(gofmt -l .); if [ -n "$$bad" ]; then echo "gofmt needed:"; echo "$$bad"; exit 1; fi
	@echo "gofmt clean."

# Unit tests with the race detector, matching the CI `test` job.
test-race:
	go test -short ./... -race

# go.mod/go.sum are tidy and committed.
tidy-check:
	go mod tidy
	git diff --exit-code go.mod go.sum

# Quick fuzz pass over every fuzz target wired into CI. Override duration with
# FUZZTIME=30s for a deeper local sweep before resolving a phase.
FUZZTIME ?= 10s
fuzz:
	go test ./internal/shell/ -fuzz=FuzzQuote -fuzztime=$(FUZZTIME)
	go test ./internal/config/ -fuzz=FuzzParseUpdateInterval -fuzztime=$(FUZZTIME)
	go test ./internal/invocation/ -fuzz=FuzzParseName -fuzztime=$(FUZZTIME)
	go test ./internal/parse/ -fuzz=FuzzImageDigest -fuzztime=$(FUZZTIME)

# Vulnerability scan using the same policy as CI: fail only on vulnerabilities
# that have a fix available; unfixable upstream advisories are reported, not fatal.
vuln:
	@PATH="$$(go env GOPATH)/bin:$$PATH"; \
	command -v govulncheck >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest; \
	govulncheck ./... > /tmp/ai-shim-vuln.out 2>&1; rc=$$?; \
	cat /tmp/ai-shim-vuln.out; \
	if [ $$rc -ne 0 ] && ! grep -q "Vulnerability #" /tmp/ai-shim-vuln.out; then \
		echo "ERROR: govulncheck did not run cleanly (rc=$$rc)"; exit 1; fi; \
	fixable=$$(awk '/^Vulnerability #/{v=$$0} /Fixed in:/{if($$NF!="N/A") print v}' /tmp/ai-shim-vuln.out); \
	if [ -n "$$fixable" ]; then echo "ERROR: fixable vulnerabilities found — upgrade:"; echo "$$fixable"; exit 1; fi; \
	echo "vuln: OK (no fixable vulnerabilities)."

# Full local mirror of the CI gate, minus the Docker e2e job (`make e2e-ci`)
# and golangci-lint (`make lint`, needs the binary). Run before resolving a phase.
ci: fmt-check vet tidy-check check-silent-failures test-race fuzz vuln

setup:
	@command -v lefthook >/dev/null || (echo "Installing lefthook..." && go install github.com/evilmartians/lefthook@latest)
	lefthook install
