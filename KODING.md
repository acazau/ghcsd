# Development Guidelines

## Build/Test/Lint
- **Build**: `go build ./...`
- **Test All**: `go test ./...`
- **Run Single Test**: `go test -run TestName pkg/path`
- **Lint**: `golangci-lint run --fast`

## Code Style
- **Imports**:
  - Grouped as: stdlib → 3rd party → local packages
  - No blank imports
- **Formatting**: `gofmt -s` (enforced)
- **Naming**:
  - Public: `CamelCase`, Private: `snake_case`
  - Types/Interfaces: `Capitalized`
- **Error Handling**:
  - Explicit checks, no ignored errors
  - Use `fmt.Errorf("message: %w", err)` for wrapping

## Conventions
- HTTP handlers return `http.ResponseWriter` and `*http.Request`
- Logger outputs to `log.StandardLogger()`
- Configuration via `internal/config`
