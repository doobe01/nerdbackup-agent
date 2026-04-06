# Contributing to NerdBackup Agent

Thanks for your interest in contributing! Here's how to get started.

## Development Setup

1. **Go 1.22+** — [Install Go](https://go.dev/dl/)
2. **Restic** — `brew install restic` (macOS) or [download](https://restic.net/)
3. **Clone and build:**
   ```bash
   git clone https://github.com/doobe01/nerdbackup-agent.git
   cd nerdbackup-agent
   make build
   ```

## Running Tests

```bash
make test          # Run all tests
make coverage      # Run tests with coverage report
make lint          # Run golangci-lint
make fmt           # Check formatting
make vet           # Run go vet
```

## Making Changes

1. **Fork** the repository
2. **Create a branch** from `main`: `git checkout -b my-feature`
3. **Make your changes** with tests
4. **Update CHANGELOG.md** — add your change under `[Unreleased]`
5. **Run checks:** `make test && make lint`
6. **Commit** with a clear message: `feat: add bandwidth throttling for restore`
7. **Push** and open a Pull Request

## Commit Messages

We use conventional commits:
- `feat:` — new feature
- `fix:` — bug fix
- `docs:` — documentation only
- `test:` — adding or updating tests
- `refactor:` — code change that neither fixes a bug nor adds a feature
- `chore:` — build process or auxiliary tool changes

## Code Style

- Run `gofmt` before committing (or `make fmt`)
- All exported functions need documentation comments
- Error messages should be lowercase and not end with punctuation
- Wrap errors with context: `fmt.Errorf("failed to init repo %s: %w", id, err)`
- Every function doing I/O accepts `context.Context` as first parameter
- Platform-specific code uses build tags: `//go:build linux`

## Testing Guidelines

- Use table-driven tests where appropriate
- Test both success and error paths
- Mock external dependencies (exec.Command, HTTP calls)
- Aim for 80%+ coverage on `internal/` packages

## What We're Looking For

- **Bug fixes** with reproduction steps
- **New exclude presets** (e.g., for specific frameworks or tools)
- **Platform improvements** (better Windows/macOS support)
- **Documentation** improvements
- **Performance** optimizations with benchmarks

## What We're NOT Looking For

- Changes to the NerdBackup API client that break compatibility
- Adding heavy dependencies (we aim for minimal deps)
- Features that duplicate what Restic already does

## Questions?

Open an issue with the `question` label or reach out at [nerdbackup.com](https://nerdbackup.com).
