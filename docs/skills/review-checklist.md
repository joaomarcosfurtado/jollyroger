# Skill: Review Checklist (architecture compliance)

**Read when:** finishing any branch that touches Go code. Run it over the diff BEFORE committing,
together with `/code-review` and [security-review](security-review.md) (engineering bar item 5).

## Rules
- **MUST** walk every applicable box against the diff before committing.
- **MUST NOT** commit with an unchecked box that applies.
- **MUST** fix a violation by moving code to the right layer, never by weakening a check, adding
  `//nolint`, or adding an `archtest` exception without a numbered, reasoned entry in
  [diplomat-architecture](diplomat-architecture.md).

## Checklist

### Layers
- [ ] `go test ./internal/archtest/...` is green.
- [ ] For each package the diff touches, every NEW import respects the import table (archtest
      checks packages; you check intent: a `logic` import of `model` uses entity types, not ports).
- [ ] Every wire <-> model translation added lives in `internal/adapter`; no inline field mapping in
      handlers or store methods; no adapter call inside a controller.
- [ ] No `time.Now()`, logging, or I/O added to `logic/`.

### Controllers
- [ ] New controller paths return `(T, error)` using `model` sentinels; no panic for an expected
      failure; every new sentinel has a registry row ([error-handling](error-handling.md)).
- [ ] Role / token scope re-checked in the controller; audit written in the same transaction.
- [ ] Write use cases run write + audit + revision bump inside one `InTx`.

### Dependencies and state
- [ ] No new package-level mutable state; dependencies arrive through constructors or deps structs.
- [ ] No new core-module dependency (or a written justification in the PR).
- [ ] Goroutines started by the library have an owner, a stop path via `Close()`, and a test.

### Contracts
- [ ] New `wire/in` shapes validated in production; new `wire/out`/`wire/db` asserted in adapter
      tests with exact equality.
- [ ] Added a field? The full chain in [diplomat-architecture](diplomat-architecture.md) is done,
      both directions, both dialects.
- [ ] `protocol/openapi.yaml`, `protocol/evaluation-spec.md`, `protocol/testdata/vectors.json` and
      `protocol/storage-contract.md` updated where behaviour changed.

### Public API
- [ ] Every new exported identifier is intentional, documented, and has an example or test.
- [ ] No breaking change to an exported identifier without a major version plan; CHANGELOG entry
      added under `Unreleased`.

### Tests and gate
- [ ] Store change: integration tests on PostgreSQL AND SQLite, `storetest` case added.
- [ ] Fix: regression test fails before, passes after.
- [ ] Full gate green: build, vet, test, race, integration, golangci-lint, govulncheck.

## Related
- [diplomat-architecture](diplomat-architecture.md) · [code-organization](code-organization.md) ·
  [security-review](security-review.md) · [testing-standards](testing-standards.md)
