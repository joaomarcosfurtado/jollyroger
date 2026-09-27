# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). Before v1.0.0 a minor release may
contain breaking changes; they are listed under `### Breaking`.

## [Unreleased]

### Added
- Evaluation engine: compiled, immutable snapshots evaluated with zero allocations; deterministic
  SHA-256 bucketing.
- Protocol: `protocol/evaluation-spec.md` and `protocol/testdata/vectors.json`, the cases every
  SDK must pass.
- Snapshot decoding that every SDK can reproduce: case-sensitive field names, null as absent, and
  a config the reader cannot decode isolated to its own flag (`PARSE_ERROR`).
- Input validation for flag keys, environment keys, names, descriptions, reasons and configs.
- Design document for v0.1.0 (`docs/superpowers/specs/2026-09-26-jollyroger-design.md`).
- Repository bootstrap: engineering rules (`CLAUDE.md`), contributor skills (`docs/skills`),
  architecture import guard (`internal/archtest`), CI (build, vet, race, integration, lint,
  govulncheck, gitleaks), and local PostgreSQL via docker compose.
