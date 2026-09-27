# Skill: Diplomat Architecture (in Go)

**Read when:** starting any package, adding an endpoint or use case, or answering "where does this
code go?". jollyroger follows the Diplomat architecture (a functional take on hexagonal /
ports and adapters), translated to Go idioms.

## Rules
- **MUST** route every request through the full path: `wire/in` (decode + `Validate()`) ->
  `adapter/api` -> `controller` -> (`logic` / model ports) -> `adapter/api` -> `wire/out` ->
  JSON encoder or `diplomat/ui` template.
- **MUST** keep model types (`internal/model`) as the only shapes `controller/` and `logic/` see.
  They never see an `*http.Request`, a `*sql.Rows`, or a wire struct.
- **MUST** do every wire <-> model translation in `internal/adapter/...`, the only legal translation
  point. No field-by-field mapping inside a handler or a store method.
- **MUST** obey the import table below; `internal/archtest` fails the suite on a violation.
- **MUST** thread every dependency (stores, clock, logger, metrics) through constructors. No
  package-level mutable state (CLAUDE.md rule 1).
- **MUST** return `(T, error)` from controllers, with sentinel errors from `internal/model` for
  expected failures; never panic for a business failure (CLAUDE.md rule 2).
- **MUST NOT** call `time.Now()`, log, or touch I/O inside `logic/`. Take a `time.Time` argument.
- **MUST** put each new port method on an interface in `internal/model` and implement it in EVERY
  store (`postgres`, `sqlite`, `memory`) plus a `storetest` conformance case, in the same change.
- **MUST NOT** add a new layer crossing outside the sanctioned exceptions listed below. A new
  exception needs a numbered entry here with its reason, and an `archtest` update.

## The layer map

```
jollyroger (root)          composition root + public facade: New, options, Client, Handler
cmd/jollyroger             CLI composition root (drives controllers like a handler does)
internal/
  model/                   DOMAIN: entity structs, sentinel errors, PORT interfaces
  logic/                   DOMAIN: pure functions (eval, snapshot, validate, pagination)
  controller/              DOMAIN: one file per use case (create_flag.go, set_flag_state.go, ...)
  wire/in/                 BOUNDARY: untrusted request shapes + Validate()
  wire/out/                BOUNDARY: response + view shapes (asserted in adapter tests); protocol
                           documents such as the snapshot are also read, with their decoding rules
                           in wire/out/decode.go
  wire/db/                 BOUNDARY: row scan targets per table
  adapter/api/            TRANSLATION: wire/in -> model command, model -> wire/out
  adapter/db/              TRANSLATION: wire/db row -> model, model -> SQL args
  diplomat/                I/O: postgres, sqlite, memory, migrate, httpserver, ui, poller, cache
internal/storetest/        conformance suite every store runs (public once the store port is)
jollyrogertest/            PUBLIC test helper for adopters (fixed flags, no DB)
```

## The canonical flows

```
HTTP request
  -> diplomat/httpserver handler decodes into wire/in.X, calls X.Validate()   (fail closed: 400/422)
  -> adapter/api.XToCommand(wireIn)                                           (pure)
  -> controller.DoX(ctx, deps, cmd) (T, error)
       -> logic.* (pure decisions)
       -> model port (FlagStore.InTx ...) implemented in diplomat/postgres
            -> SQL -> wire/db row -> adapter/db.RowToFlag(row) -> model.Flag
  -> adapter/api.FlagToOut(model)  -> wire/out.Flag -> json.Encoder / ui template
```

Three invariants that must never break:
1. **Models live only in the domain.** `controller/` and `logic/` handle `model.*` types only.
2. **Wires live only at the boundary.** `controller/` and `logic/` never import `internal/wire`.
3. **Adapters are the only translation point.** A store method returns `adapter/db.RowToX(row)`, it
   never assembles a model field by field inline.

## Import table (enforced by `internal/archtest`)

| Layer | May import (internal) | Must NOT import |
|---|---|---|
| `model` | nothing | everything internal |
| `wire/*` | nothing | everything internal |
| `logic/*` | `logic`, `model` | `controller`, `wire`, `adapter`, `diplomat` |
| `controller` | `model`, `logic` | `wire`, `adapter`, `diplomat` |
| `adapter/*` | `adapter` [5], `model`, `wire`, `logic` [2] | `controller`, `diplomat` |
| `diplomat/*` | `diplomat`, `adapter`, `wire`, `model`, `logic` [3] | `controller` (except entry points [4]) |
| `internal/storetest` | `model`, `logic` | everything else internal |
| root, `cmd/`, `jollyrogertest` | anything | (composition roots) |

Pure layers (`model`, `wire`, `logic`, `adapter`) also must not import I/O packages from the
standard library: `database/sql`, `net`, `net/http`, `os`, `os/exec`, `syscall`, `log`, `log/slog`.
`controller` must not import the same I/O packages, except `log/slog` (it may take a
`*slog.Logger` through its deps struct). The authoritative list is `isIOPackage` in
`internal/archtest/rules.go`.

### Sanctioned exceptions (the only crossings allowed)
- **[1]** Root and `cmd/` are composition roots: they wire everything together.
- **[2]** `adapter/*` may call a PURE `logic` helper for formatting or encoding (e.g. a pagination
  cursor encoder). Never a `logic` function that makes a business decision.
- **[3]** `diplomat/*` may call a PURE `logic` helper to BUILD a query or payload (e.g. cursor
  decode). The decision stays in `logic`/`controller`.
- **[4]** Entry points drive controllers: `diplomat/httpserver` (HTTP) and `diplomat/poller`
  (timer). Stores, migrate, ui and cache must not import `controller`.
- **[5]** `adapter/*` may import another `adapter/*` package: composing pure translations stays
  pure (for example `adapter/db` stores the protocol config JSON through `adapter/api`).

## Go idiom mapping (from the Clojure original)

| Diplomat concept | Go form here |
|---|---|
| `{:ok v}` / `{:error :kw}` | `(T, error)`; `errors.Is(err, model.ErrNotFound)` |
| Protocol | interface in `internal/model` |
| `reify` fake in a test | a small struct in the `_test.go` file implementing only what the test needs |
| Malli schema | a struct with a `Validate() error` method (`wire/in`) or an adapter-test assertion |
| Stuart Sierra component | a constructor returning a struct + `Close()`; wired in the root `New` |

## Adding a field: the full chain (a missing link silently drops the value)

1. migration (`ADD COLUMN IF NOT EXISTS`, both dialects) -> [migrations](migrations.md)
2. `wire/db` row struct + the SELECT column list in every store
3. `adapter/db` BOTH directions (row -> model and model -> args)
4. `model` entity field
5. controller command/result
6. `wire/in` field + `Validate()`, `adapter/api` both directions, `wire/out` field
7. `protocol/openapi.yaml`, and `protocol/storage-contract.md` if SDKs may read it
8. `storetest` round-trip case; adapter tests assert the full shape with exact equality

The round-trip conformance case is the cheapest guard: write a value, read it back through the
real store, compare. It catches a forgotten column in a SELECT that no unit test can see.

## The four "do not couple" rules
1. Do not couple logic with side effects: compose them in the controller.
2. Do not couple controllers with entry points: a controller never knows it was called from HTTP,
   the CLI, or the poller.
3. Do not couple diplomat with domain rules: no business decision inside a store or handler.
4. Do not couple wire shapes with models: adapters are the only translation point.

## Why the ceremony is worth it here
- **Adopters see none of it.** Everything is under `internal/`; the public API stays tiny.
- **Pure layers are cheap to test exhaustively**, which matters for an evaluation engine whose
  behaviour every future SDK must reproduce bit for bit.
- **Swapping storage touches only `diplomat/<store>` + `wire/db`.** That is exactly the property a
  library supporting two databases (and more later) needs.

## Related
- [code-organization](code-organization.md) · [review-checklist](review-checklist.md) ·
  [testing-standards](testing-standards.md) · [error-handling](error-handling.md)
