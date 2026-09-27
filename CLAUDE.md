# jollyroger: Project Instructions

jollyroger is an open-source, embedded feature-flag library for Go. It runs inside the host
application, stores flags in the host's existing PostgreSQL or SQLite database, evaluates them from
an in-process cache, and ships an opt-in, authenticated dashboard mounted on the host's own router.

> **Feature flags without another server to maintain.**

The target is **real adoption by other teams**, not a demo. Every decision favours API stability,
security, portability and developer experience over feature count.

---

## Tech stack

| Concern | Choice |
|---|---|
| Language | Go (supports the last two Go releases; `go.mod` names the older one) |
| Storage | `database/sql` handed in by the host; hand-written SQL per dialect (PostgreSQL, SQLite); no ORM |
| Dashboard | `html/template` + htmx (vendored, embedded via `embed.FS`); no Node toolchain |
| HTTP | `net/http` only; works under net/http, chi, gin, echo |
| Password hashing | argon2id (`golang.org/x/crypto`) |
| Observability | `log/slog` + a `Metrics` port; Prometheus / OpenTelemetry only in `contrib/` modules |
| Tests | `go test`, `-race`, real PostgreSQL (docker compose) + SQLite (`modernc.org/sqlite`, test-only) |

The **core module depends on the standard library plus `golang.org/x/crypto`**. Anything else goes in
a separate module under `contrib/` or is test-only. Adding a core dependency needs a written reason
in the PR.

---

## Architecture: Diplomat (ports and adapters), translated to Go

```
HTTP / CLI / poller (entry points, internal/diplomat/httpserver, cmd/, internal/diplomat/poller)
  -> internal/wire/in   (decode + Validate(), fail closed)
  -> internal/adapter   (wire -> model, the ONLY translation point)
  -> internal/controller (one file per use case: orchestrate ports + logic, return (T, error))
       -> internal/logic  (pure: evaluation, validation, pagination)
       -> internal/model  ports, implemented by internal/diplomat/{postgres,sqlite,memory}
  -> internal/adapter   (model -> wire/out)
  -> internal/wire/out  -> JSON encoder or internal/diplomat/ui template -> response
```

- **Model** (`internal/model`): entity structs, sentinel errors, port interfaces. Stdlib only.
- **Logic** (`internal/logic/...`): pure functions. No I/O, no clock reads, no logging.
- **Controller** (`internal/controller`): orchestration. Never sees HTTP, SQL, or wire structs.
- **Wire** (`internal/wire/{in,out,db}`): boundary shapes. Stdlib only.
- **Adapter** (`internal/adapter/{api,db}`): pure wire <-> model translation.
- **Diplomat** (`internal/diplomat/...`): all I/O (stores, migrations, HTTP server, UI, poller, cache).
- **Root package** `jollyroger` + `cmd/jollyroger`: composition roots and the public facade.

The layer import table is enforced by `internal/archtest`. Read
`docs/skills/diplomat-architecture.md` before writing any code.

---

## Skills: read the one that matches what you touch

All skills live in `docs/skills/`. Each is the single source of truth for its area.

| Skill | Read when... |
|---|---|
| [diplomat-architecture](docs/skills/diplomat-architecture.md) | Starting any package; any question about layer boundaries |
| [code-organization](docs/skills/code-organization.md) | Creating any file or package; public vs `internal/` API |
| [testing-standards](docs/skills/testing-standards.md) | Writing any test; conformance suites; integration tests |
| [migrations](docs/skills/migrations.md) | Writing or changing any SQL migration |
| [error-handling](docs/skills/error-handling.md) | Returning or mapping an error |
| [secure-by-design](docs/skills/secure-by-design.md) | Touching auth, sessions, input, SQL, headers, templates, secrets |
| [api-security](docs/skills/api-security.md) | Touching the JSON API, tokens, CORS, rate limits |
| [security-review](docs/skills/security-review.md) | Finishing any branch (pre-merge security checklist) |
| [exploratory-security](docs/skills/exploratory-security.md) | Attacking a feature like an adversary with the `test/security` harness |
| [review-checklist](docs/skills/review-checklist.md) | Finishing any branch (architecture-compliance checklist) |
| [logging](docs/skills/logging.md) | Adding a log line |
| [observability](docs/skills/observability.md) | Adding metrics, traces, hooks, health, or a `contrib/` adapter |
| [git-workflow](docs/skills/git-workflow.md) | Starting any work: worktrees, branches, PRs, releases |
| [documentation](docs/skills/documentation.md) | Any change that should propagate to docs |
| [writing-skills](docs/skills/writing-skills.md) | Creating or editing a skill |
| [ticket-sdd](docs/skills/ticket-sdd.md) | Running `/create-ticket` or `/ticket` |
| [local-development](docs/skills/local-development.md) | Setting up the toolchain, running the suites, docker compose |

The approved design lives in `docs/superpowers/specs/`. Protocol contracts (API, evaluation,
storage read contract) live in `protocol/`.

---

## Non-negotiable rules

1. **Threaded dependencies, never globals.** Stores, loggers, clocks, metrics and config are passed
   to constructors and held in struct fields. No package-level mutable state, no `init()` side
   effects, no reading `os.Getenv` outside `cmd/`. The host's `slog.Default()` may be captured ONCE,
   in the root `New`, as the default logger; nothing reads it again.
2. **Controllers return `(T, error)` and never panic on an expected failure.** Business failures are
   the sentinel errors in `internal/model` (wrapped with `%w`). They map to HTTP status + problem
   JSON in exactly one registry (`error-handling.md`).
3. **Go names inside, snake_case JSON outside.** Wire structs carry the `json:"snake_case"` tags; the
   adapters are the only translation point between wire and model.
4. **All timestamps are UTC `time.Time`.** Time comes from the injected `model.Clock`, never from
   `time.Now()` inside `logic/` or `controller/`.
5. **Tests ship with the change.** Every exported behaviour and every bug fix has a test in the same
   commit. The full gate (below) must be green before merge.
6. **Migrations are idempotent and additive.** `IF NOT EXISTS` everywhere; within a major version a
   migration may only ADD (the schema is a cross-language read contract, see `protocol/`). A
   migration never changes after it ships.
7. **Every fix ships a regression test that fails before the fix and passes after.**
8. **Work in your own worktree on a named branch; `main` changes only through a reviewed PR.** See
   `git-workflow.md`.
9. **Resolve every warning; never hide one.** Zero `go vet` findings, zero golangci-lint findings,
   zero red tests, zero data races. Do not add `//nolint`, skip a test, or relax a linter to make a
   signal disappear. The ONLY allowed suppression is a genuine false positive, scoped to one linter
   on one line, with a comment explaining why it is false.
10. **Security is a default, not a feature.** The dashboard and API are off by default and never run
    without an auth mode. Validate input at the boundary and fail closed; parameterize every SQL
    value; render HTML only through `html/template`; re-check authorization in the controller, not
    only in middleware. Run `security-review.md` before finishing a branch.
11. **Wire shapes and protocol files are live contracts.** Every `wire/in` struct is validated in
    production at the boundary; every `wire/out` / `wire/db` shape is asserted in adapter tests;
    every HTTP response is validated against `protocol/openapi.yaml` in tests; every SDK runs
    `protocol/testdata/vectors.json`. A contract nothing checks is a bug: wire it in, never delete it.
12. **Store code is tested against real databases.** Any change to a migration or a
    `internal/diplomat/{postgres,sqlite}` method ships an integration test, and every store
    (including `memory`) passes the shared `storetest` conformance suite. A fake that is more
    generous than the real database proves nothing.
13. **One concern per package and per file.** Split when a second concern lands, never by line count.
    One controller use case per file, one store entity per file.
14. **The public API is small and deliberate.** Only the root package, `storetest` and
    `jollyrogertest` are importable by adopters; everything else is `internal/`. Changing an exported
    identifier is a semver event and needs a CHANGELOG entry.
15. **Never use the em-dash character in any text you write** (docs, comments, commits, PRs, UI copy).
    Use a comma, colon, parentheses, or a period.
16. **No shortcuts.** Do not downscope the correct solution because it looks like a lot of work. The
    thorough, correct implementation is the default. Match rigor to the task (a typo fix stays a typo
    fix), but never trade correctness or security for speed.
17. **English everywhere.** Code, docs, UI copy, commits and issues are English.

---

## Engineering bar (definition of done)

State this bar in the plan, then meet every item before merging. Match rigor to the task.

1. **Right layer.** Diplomat respected; `internal/archtest` green.
2. **Secure by default.** Boundary validation, parameterized SQL, escaped output, authorization
   re-checked in the controller.
3. **Tests.** Unit tests for pure layers and controllers (fakes built on the ports); real PostgreSQL
   AND SQLite integration tests for store code; conformance suite for every store; a regression test
   for every fix; benchmarks with allocation checks for the evaluation hot path.
4. **The gate is green:** `go build ./...`, `go vet ./...`, `go test ./...`,
   `go test -race ./...`, `go test -tags integration ./...` (docker compose up), golangci-lint, and
   govulncheck. Run the full gate once at task START to capture a baseline.
5. **Review before commit.** Run `/code-review` on the diff and walk `review-checklist.md` +
   `security-review.md`. Resolve or consciously dismiss (with a reason) every finding.
6. **Docs travel with the code.** Skills, `protocol/`, README and CHANGELOG are updated in the same
   branch.
7. **Workflow.** Own worktree, named branch, PR to `main`, conventional-commit title, commit trailer
   `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` on agent-authored commits.

---

## Running the gate

```bash
docker compose up -d                      # PostgreSQL for integration tests
go build ./... && go vet ./...
go test ./...
go test -race ./...
go test -tags integration ./...           # real PostgreSQL + SQLite
golangci-lint run
govulncheck ./...
```

See `docs/skills/local-development.md` for the toolchain and Windows notes.
