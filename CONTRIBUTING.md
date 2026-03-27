# Contributing

## Setup

After cloning, run:

```bash
make setup
```

This installs [lefthook](https://github.com/evilmartians/lefthook) and configures git hooks that run formatting, linting, and tests before each commit. If you don't have lefthook installed, the setup target will install it via `go install`.

## Development

```bash
make build        # Build the binary
make test         # Run all tests
make test-short   # Run unit tests only (skip E2E)
make lint         # Run golangci-lint
make fmt          # Format all Go files
make verify       # Run all checks (fmt, vet, lint, test)
```

## Pre-commit hooks

The lefthook configuration (`lefthook.yml`) runs these checks automatically on each commit:

- **gofmt** — auto-formats staged Go files
- **go vet** — catches common mistakes
- **go mod tidy** — ensures dependencies are clean
- **golangci-lint** — static analysis
- **go test -short** — runs unit tests for changed packages
- **silent failure check** — flags suppressed errors in production code
- **conventional commit** — validates commit message format

## Commit messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(config): add profile switching support
fix(container): handle signal forwarding correctly
docs: update README with new commands
test: add edge case coverage for resolver
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`
