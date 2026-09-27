# Skill: Documentation (what to update, and when)

**Read when:** any change that alters behaviour, API, schema, protocol, or a rule.

## Rules
- **MUST** update docs in the SAME branch as the code. Skills and protocol files are the source of
  truth; a follow-up docs PR is how they rot.
- **MUST** update the matching file from the table below.
- **MUST** keep every exported identifier documented with a Go doc comment that starts with its
  name, and add an `Example...` test for anything an adopter calls directly.
- **MUST** keep `README.md` quickstart runnable: if the public API changes, the README snippet and
  `examples/` change in the same PR (CI builds the examples module).
- **MUST** add a new skill to the `CLAUDE.md` skills table.
- **MUST** follow the rules-first skill format ([writing-skills](writing-skills.md)) and never use
  the em-dash character.
- **MUST** keep `docs/backlog.md` current: remove an item when it ships, add one (what + why
  deferred) when you defer or discover work. It lists only what is still to do.

## What to update

| Change | Update |
|---|---|
| Exported API added/changed | doc comments, `example_test.go`, README, `CHANGELOG.md` |
| HTTP endpoint added/changed | `protocol/openapi.yaml`, `docs/api.md`, CHANGELOG |
| Evaluation behaviour | `protocol/evaluation-spec.md`, `protocol/testdata/vectors.json`, CHANGELOG |
| Migration / table / column SDKs may read | `protocol/storage-contract.md`, [migrations](migrations.md) if a rule changed |
| Metric, hook, health field | `protocol/metrics.md`, `docs/observability.md` |
| Security-relevant behaviour | `SECURITY.md` or `docs/security.md`, relevant skill |
| New option | its doc comment (default + safe value), README options table |
| Architecture rule | [diplomat-architecture](diplomat-architecture.md) + `internal/archtest` |
| New skill | `CLAUDE.md` skills table |

## Audiences
- `README.md`, `docs/*.md` (getting-started, api, observability, security, architecture): adopters.
- `protocol/*`: SDK authors in other languages. Must be precise enough to implement from.
- `docs/skills/*`, `CLAUDE.md`, `CONTRIBUTING.md`: contributors and agents.
- `docs/superpowers/specs/*`: design decisions and their reasons.

## Related
- [writing-skills](writing-skills.md) · [git-workflow](git-workflow.md)
