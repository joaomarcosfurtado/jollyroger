# Skill: Local Development

**Read when:** setting up a machine, running the suites, or debugging the toolchain.

## Rules
- **MUST** use a Go toolchain that satisfies `go.mod`. With `GOTOOLCHAIN=auto` (the default), an
  older local Go downloads the required toolchain automatically on first use.
- **MUST** start the databases with `docker compose up -d` before integration tests; the default
  test URL matches `docker-compose.yml`.
- **MUST** run the full gate (CLAUDE.md "Running the gate") once at the start of a task to record a
  baseline, and again before opening a PR.
- **MUST NOT** commit generated binaries, coverage files, or local databases (`*.db`, `*.sqlite`
  are gitignored).

## Setup
```bash
git clone https://github.com/joaomarcosfurtado/jollyroger
cd jollyroger
go version                     # any Go >= 1.21 bootstraps the toolchain named in go.mod
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go install golang.org/x/vuln/cmd/govulncheck@latest
docker compose up -d           # PostgreSQL on localhost:55432
```

## Environment for tests

| Variable | Default | Used by |
|---|---|---|
| `JOLLYROGER_TEST_POSTGRES_URL` | `postgres://jollyroger:jollyroger@localhost:55432/jollyroger_test?sslmode=disable` | integration tests |

SQLite integration tests use a temporary file per test (pure Go driver, no cgo needed).

## Windows notes
- Use Git Bash or PowerShell; the commands above are the same.
- The compose file maps PostgreSQL to port 55432 to avoid clashing with a local 5432.
- If `go test -race` reports "cgo required", install a C toolchain (e.g. via MSYS2 mingw-w64) or
  rely on CI for the race run; the race detector needs cgo on Windows.

## Related
- [testing-standards](testing-standards.md) · [git-workflow](git-workflow.md)
