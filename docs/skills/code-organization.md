# Skill: Code Organization

**Read when:** creating any file or package, deciding what is public, or splitting code.

## Rules
- **MUST** place every file per the layer map in [diplomat-architecture](diplomat-architecture.md).
- **MUST** keep ONE concern per package and per file. Split when a second concern lands (a second
  independent API, a pure helper welded to I/O, one-more-entity each feature), never by line count.
- **MUST** put one controller use case per file (`internal/controller/set_flag_state.go`) and one
  store entity per file (`internal/diplomat/postgres/flags.go`, `.../audit.go`).
- **MUST** ship `x_test.go` next to every `x.go` that has behaviour, except the named exemptions
  below. Coverage for an exempt file must live in a specific, named place.
- **MUST** keep the public API to the root package, `storetest` and `jollyrogertest`. Everything
  else lives under `internal/`. Re-export what adopters need from the root as type aliases
  (`type Context = model.Context`).
- **MUST** put optional integrations that bring dependencies in their own Go module under
  `contrib/<name>/` (own `go.mod`), and examples in the separate `examples/` module, so the core
  module's dependency list stays stdlib + `golang.org/x/crypto`.
- **MUST** name packages singular, lower-case, no underscores (`controller`, not `controllers`).
- **MUST NOT** create `util`, `common`, `helpers` or `misc` packages. A helper belongs to the layer
  whose concern it serves.
- **MUST NOT** declare package-level `var`s holding mutable state. Package-level values are limited
  to constants, sentinel errors, compiled regexps and `embed.FS`.
- **MUST** keep SQL for each dialect inside that dialect's store package. Shared SQL text across
  dialects is a coincidence, not an abstraction; do not factor it into a "common SQL" package.
- **MUST** use `internal/diplomat/<store>/migrations/*.sql` embedded with `//go:embed` for schema.

## Test-file mirror exemptions (coverage lives elsewhere, BY RULE)

| Exempt | Where its coverage lives |
|---|---|
| `internal/model` interface declarations | every implementation + the `storetest` suite |
| `internal/wire/out/**`, `internal/wire/db/**` | the adapter test for that entity asserts the shape |
| `internal/diplomat/{postgres,sqlite}/**` store methods | `storetest` run from `integration`-tagged tests against real databases |
| `doc.go` files | nothing to test |

`internal/wire/in/**` is NOT exempt: it is the untrusted-input gate, the `Validate()` method IS the
security control, and it gets its own table-driven test. Adding a row here is a claim that coverage
lives somewhere specific: name the place and check it is real first.

## Public API checklist (every exported identifier is a promise)
- Would an adopter need this? If not, keep it in `internal/`.
- Is it documented with a doc comment that starts with its name?
- Does it appear in an example (`example_test.go`) so `pkg.go.dev` shows usage?
- Removing or changing it later is a major version. Prefer adding an option over changing a
  signature: functional options (`With...`) exist so `New` never needs a breaking change.

## Splitting: the two traps
1. **Never split one interface's implementation across unrelated files by "size".** Keep a store's
   methods for one entity together; split by entity instead.
2. **When you split a package, update `internal/archtest` rules if the layer changes**, and re-run
   the full gate: a moved type can silently change which layer imports it.

## Related
- [diplomat-architecture](diplomat-architecture.md) · [testing-standards](testing-standards.md) ·
  [documentation](documentation.md)
