# Contributing to jollyroger

Thanks for helping. jollyroger runs inside other people's applications, so the bar for correctness,
security and API stability is high. This guide tells you how to clear it.

## Before you start
- For anything bigger than a small fix, open an issue first so we can agree on the approach.
- Read [`CLAUDE.md`](CLAUDE.md): it is the project's constitution (rules + definition of done), for
  humans and AI agents alike.
- Read the skill that matches what you touch, in [`docs/skills`](docs/skills). Start with
  [diplomat-architecture](docs/skills/diplomat-architecture.md).

## Development setup
See [local-development](docs/skills/local-development.md). In short:

```bash
docker compose up -d
go build ./... && go vet ./...
go test -race ./...
go test -tags integration ./...
golangci-lint run
```

## Pull requests
- One concern per PR. Branch from `main` (`feat/...`, `fix/...`, `docs/...`).
- Title in Conventional Commits form: `feat(eval): percentage rollout`, `fix(store/sqlite): ...`.
  Breaking changes use `!` (`feat!: ...`) and a `### Breaking` CHANGELOG note.
- Tests ship with the change; a fix includes a test that fails without it.
- Store changes are tested against real PostgreSQL AND SQLite.
- Update docs in the same PR (skills, `protocol/`, README, CHANGELOG `Unreleased`).
- Walk [review-checklist](docs/skills/review-checklist.md) and
  [security-review](docs/skills/security-review.md) before requesting review.
- No new dependency in the core module without discussing it first.
- Please do not use the em-dash character in text (a project style rule).

## Reporting bugs
Open an issue with the Go version, database and version, a minimal reproduction, and what you
expected. Security issues go through [SECURITY.md](SECURITY.md), never a public issue.

## License
By contributing you agree that your contributions are licensed under the Apache License 2.0.
