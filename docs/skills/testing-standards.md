# Skill: Testing Standards

**Read when:** writing any test, adding a store method, fixing a bug, or touching the evaluation
hot path.

## Rules
- **MUST** ship tests with the change (CLAUDE.md rule 5). Every exported behaviour and every
  controller path (success AND each error) has a test.
- **MUST** ship every bug fix with a regression test that FAILS before the fix and PASSES after
  (rule 7). Run it against the unfixed code once to prove it catches the bug.
- **MUST** run the FULL gate once at task start to record a baseline, and again before merge. A
  hand-picked `go test ./internal/logic/...` is not a green build.
- **MUST** test pure layers (`logic`, `adapter`, `wire/in`) with table-driven tests, no fakes needed.
- **MUST** test controllers with small fakes built on the `internal/model` port interfaces, defined
  in the `_test.go` file, implementing only the methods that test needs.
- **MUST** make every store (`postgres`, `sqlite`, AND `memory`) pass the shared `storetest`
  conformance suite. When a real-DB bug is found that the suite missed, add the case to `storetest`
  so every store is held to it, and fix the `memory` store if it was more generous.
- **MUST** run store tests against REAL PostgreSQL and SQLite (build tag `integration`) for any
  change to a migration or a store method (rule 12). A fake cannot enforce NOT NULL, UNIQUE, FK,
  CHECK, triggers, transaction isolation or driver type conversion.
- **MUST** pass `go test -race ./...`. Concurrency code (cache, poller, rate limiter) ships a test
  that exercises concurrent readers and writers.
- **MUST** keep the evaluation hot path benchmarked with `b.ReportAllocs()`, and a test asserting
  `testing.AllocsPerRun(...) == 0` for `Client.Enabled(key)`, so an allocation regression fails CI.
- **MUST** run the evaluation engine against `protocol/testdata/vectors.json`; a change in behaviour
  is a change to that file, reviewed as a protocol change.
- **MUST** assert adapter output with exact equality on the whole struct (`reflect.DeepEqual` or
  `cmp`-style), not field by field, so an added field that is not mapped fails the test.
- **MUST** use `t.Cleanup` / per-test schemas or databases so integration tests are independent and
  can run with `-p` parallelism. Never depend on test order or leftover rows.
- **MUST NOT** use `time.Sleep` to wait for asynchronous behaviour. Inject the clock and a trigger
  channel, or poll a condition with a deadline.
- **MUST NOT** skip, comment out, or loosen an assertion to go green (rule 9).

## Why the fakes are not enough (the lessons)
A green unit suite proves nothing where the fake is more generous than reality. The source project
shipped four bugs in one session through a green suite, each because a test double was kinder than
the real thing: a store that accepted `NULL` for a NOT NULL column; a fake that stored real
`time.Time` values while the driver returned a different type; a fake gateway that auto-approved so
the "not approved" path was never exercised; a handler that forgot to pass a dependency while every
controller test built its own complete deps. And the mirror image: a fake that *threw* on failure
while the real port *returned false*, so the error-handling branch was dead code in production.

The questions a unit test structurally cannot ask:
1. **What did the real database/driver return?** -> integration tests + `storetest`.
2. **What did the HANDLER actually assemble?** -> handler tests that go through the real composition
   (the root `New` or the `httpserver` constructor), asserting the persisted row, not just the 200.
3. **How does the real port signal failure?** -> the fake must fail the SAME way (return value vs
   error vs panic). Check before writing the fake.

Therefore the fake is a simulator, not a stub: the `memory` store enforces uniqueness, versions,
archived state and audit immutability exactly like the SQL stores, and `storetest` proves it.

## Test types by layer

| Layer | Test type | Needs a DB |
|---|---|---|
| `internal/logic/*` | table-driven, property-style where useful (`testing/quick` or fuzz) | no |
| `internal/wire/in` | table-driven valid + invalid inputs for `Validate()`; `Fuzz` for decoders | no |
| `internal/adapter/*` | exact-equality mapping tests; wire/out shape assertions | no |
| `internal/controller` | fakes on the ports; every error branch | no |
| `internal/diplomat/memory` | `storetest.Run` | no |
| `internal/diplomat/{postgres,sqlite}` | `storetest.Run` + dialect specifics, tag `integration` | yes |
| `internal/diplomat/migrate` | apply twice (idempotent), concurrent apply (lock), checksum drift | yes |
| `internal/diplomat/httpserver` | `httptest` through the real router; OpenAPI response validation | memory store |
| root package | end to end: `New` + `Handler` + `Enabled` with memory, then with real DBs | both |
| `test/security` | the attack harness, see [exploratory-security](exploratory-security.md) | both |

## Conventions
- Test names describe behaviour: `TestSetFlagState_VersionMismatch_ReturnsConflict`.
- Table-driven cases use `t.Run(tc.name, ...)` and call `t.Parallel()` when safe.
- Fixtures build model values with small helper functions in the `_test.go` file, not shared
  global fixtures.
- Integration tests read `JOLLYROGER_TEST_POSTGRES_URL` (default matches `docker-compose.yml`);
  if it is unset AND the database is unreachable they fail with a clear message, they never skip
  silently in CI.

## Commands
```bash
go test ./...                               # unit
go test -race ./...                         # race detector
go test -tags integration ./...             # real PostgreSQL + SQLite (docker compose up -d first)
go test -run XXX -bench . -benchmem ./...   # benchmarks
```

## Related
- [diplomat-architecture](diplomat-architecture.md) · [migrations](migrations.md) ·
  [exploratory-security](exploratory-security.md) · [local-development](local-development.md)
