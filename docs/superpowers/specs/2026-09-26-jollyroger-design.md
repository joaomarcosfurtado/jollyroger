Status: Approved

# jollyroger: design

**Feature flags without another server to maintain.** An open-source, embedded feature-flag library
for Go that runs inside the host application, stores flags in the host's existing PostgreSQL or
SQLite database, evaluates them in memory, and ships an opt-in, authenticated dashboard.

This document is the binding design for v0.1.0. It is governed by `CLAUDE.md` (the constitution)
and the skills in `docs/skills/`; where they conflict, the constitution wins and this document is
fixed.

---

## 1. Goals, non-goals, success criteria

**Primary goal:** real adoption by other teams. That means a tiny, stable public API, safe defaults,
excellent docs, and behaviour a team can trust in production.

Goals for v0.1.0:
- Add flags to an existing Go service with one `New` call, one `Handler` mount, and the database it
  already has.
- Evaluate flags in-process with zero allocations and no network or database call per evaluation.
- Keep multiple instances consistent (eventually, within one poll interval) with no extra
  infrastructure.
- Keep serving the last known flags when the database is unavailable, with visible staleness.
- A protected dashboard and a versioned JSON API, both off by default.
- An immutable audit log of every administrative change.
- Protocol documents precise enough that SDKs in other languages can be written from them.

Non-goals for v0.1.0 (explicitly out of scope): percentage rollout, targeting, segments, schedules
and variants (the model is designed for them, section 6); push invalidation (LISTEN/NOTIFY, SSE);
SDKs in other languages; OpenFeature provider; sidecar mode; analytics; Redis, Kafka, Kubernetes
operators; any hosted service.

Success criteria for v0.1.0:
1. The README quickstart works end to end on PostgreSQL and SQLite in under five minutes.
2. `Enabled(key)` benchmarks at 0 allocs/op and under 50 ns/op on the CI runner.
3. Two clients on one database converge within one poll interval (integration test).
4. The `test/security` harness matrix passes with zero P0/P1 findings.
5. Full gate green on the two supported Go versions.

## 2. Architecture

```
+--------------------------- host application ----------------------------+
|                                                                          |
|  app code -- client.Enabled("new-checkout") --+                          |
|                                               v                          |
|  +------------------------ jollyroger ------------------------------+   |
|  | Client (root facade)                                              |   |
|  |   snapshot cache: atomic.Pointer[snapshot]  <-- poller (timer)    |   |
|  |   evaluation: internal/logic/eval (pure)                          |   |
|  | Handler() under the base path (opt-in)                            |   |
|  |   dashboard (html/template + htmx) | JSON API v1 | auth, CSRF      |   |
|  | controllers: validate -> tx { write + audit + revision bump }     |   |
|  | stores: postgres | sqlite | memory                                |   |
|  +--------------------------------+----------------------------------+   |
|                                   v                                      |
|                 host's existing database (jollyroger_* tables)           |
+--------------------------------------------------------------------------+
```

The code follows Diplomat (ports and adapters) as documented in
`docs/skills/diplomat-architecture.md`, enforced by `internal/archtest`:

| Layer | Package(s) | Role |
|---|---|---|
| model | `internal/model` | entities, sentinel errors, port interfaces |
| logic | `internal/logic/{eval,snapshot,validate,pagination}` | pure functions |
| controller | `internal/controller` | one file per use case, `(T, error)` |
| wire | `internal/wire/{in,out,db}` | boundary shapes |
| adapter | `internal/adapter/{api,db}` | the only wire <-> model translation |
| diplomat | `internal/diplomat/{postgres,sqlite,memory,migrate,cache,ui}` | I/O |
| diplomat entry points | `internal/diplomat/{httpserver,poller}` | drive controllers |
| composition roots | root `jollyroger`, `cmd/jollyroger`, `jollyrogertest` | wiring, public API |
| public test kit | `storetest` | store conformance suite |

Optional integrations live in separate Go modules (`contrib/prometheus`, `contrib/otel`); examples
live in the separate `examples/` module. The core module depends on the standard library and
`golang.org/x/crypto` only. The host passes a `*sql.DB`; jollyroger imports no database driver.

### 2.1 Two planes, many languages

- **Evaluation plane (embedded in every language).** An SDK is the engine + a snapshot loader + a
  poller. It reads the `jollyroger_*` tables through its own driver, polls the revision row, and
  evaluates locally. Go is first; the order after v0.1.0 is JVM (Java, which also serves Clojure and
  Kotlin), TypeScript/Node, .NET, Python.
- **Admin plane (written once, in Go).** Dashboard, API, auth, audit and migrations. A non-Go host
  gets it from a Go service that shares the database, from the `jollyroger serve` binary (run as a
  small process, a sidecar, or only when someone changes flags), or from the CLI.

Three versioned public contracts live in `protocol/`: the storage read contract, the evaluation
spec with `testdata/vectors.json`, and `openapi.yaml`. SDKs never write and never migrate.

Refinements made while planning M1: compiling and evaluating a snapshot are one concern, so
`logic/snapshot` is part of `logic/eval`; and the HTTP translation package is `adapter/api`,
because a package named `http` would shadow `net/http` wherever both are used.

## 3. Public Go API

```go
client, err := jollyroger.New(ctx,
    jollyroger.WithPostgres(db),              // or WithSQLite(db)
    jollyroger.WithEnvironment("production"), // required: the environment this client evaluates
    jollyroger.WithDashboard(jollyroger.HostAuth(authFn)),
)
if err != nil { ... }
defer client.Close()

mux.Handle("/__flags/", client.Handler())

client.Enabled("new-checkout")                                   // bool
client.Enabled("new-checkout", jollyroger.Context{UserID: "123"}) // bool
res := client.Evaluate("new-checkout", evalCtx)                   // Result
st := client.Status()                                             // health
```

Rules of the API:
- `New(ctx, opts...) (*Client, error)`. It validates options, runs migrations (unless
  `WithoutAutoMigrate`), and loads the first snapshot. Any failure returns an error: the host sees
  it at startup (fail closed). `WithBootstrap(snapshotJSON)` / `WithSnapshotFile(path)` allow a
  fail-soft start from a saved snapshot.
- Exactly one of `WithPostgres(*sql.DB)` / `WithSQLite(*sql.DB)` is required. `WithEnvironment` is
  required; an unknown environment key is an error from `New`.
- `Enabled(key string, ctx ...Context) bool` uses at most one context; extra arguments are ignored
  (documented). It never panics and never blocks on I/O.
- `Evaluate(key string, ctx Context) Result` returns `Value`, `Reason`, `FlagVersion`, `ErrorCode`.
- An unknown or archived flag yields `false`, `Reason=ERROR`, `ErrorCode=FLAG_NOT_FOUND`, a counter
  increment, and a rate-limited warning.
- `Handler() http.Handler` owns every path under the base path (default `/__flags`,
  `WithBasePath`). It matches on the full `r.URL.Path`, so it works with net/http, chi `Mount`,
  `gin.WrapH` and `echo.WrapHandler` without prefix stripping. With neither the dashboard nor the
  API enabled, it answers 404 to everything.
- `Close()` stops the poller and waits for it. The `*sql.DB` belongs to the host and is not closed.
- Public types are re-exported from `internal/model` as aliases: `Context`, `Result`, `Reason`,
  `ErrorCode`, `Principal`, `Role`, `Status`, `Stats`, `AuditEntry`, `Hooks`, `Metrics`.

Options (all functional; adding one is never a breaking change):
`WithPostgres`, `WithSQLite`, `WithSchema` (PostgreSQL), `WithEnvironment`, `WithProject`
(default `default`), `WithoutAutoMigrate`, `WithPollInterval` (default 5s, min 1s),
`WithBootstrap`, `WithSnapshotFile`, `WithBasePath`, `WithDashboard(AuthMode)`, `WithAPITokens`,
`WithBootstrapAdmin`,
`WithTrustedProxies`, `WithInsecureCookies` (local HTTP only), `WithLogger`, `WithMetrics`,
`WithHooks`, `WithClock` (tests).

## 4. Ports (`internal/model`)

```go
type FlagStore interface {
    LoadSnapshot(ctx context.Context, project, env string) (Snapshot, error)
    Revision(ctx context.Context, project string) (int64, error)
    ListEnvironments(ctx context.Context, project string) ([]Environment, error)
    ListFlags(ctx context.Context, q FlagQuery) (FlagPage, error)
    GetFlag(ctx context.Context, project, key string) (FlagWithStates, error)
    ListAudit(ctx context.Context, q AuditQuery) (AuditPage, error)
    InTx(ctx context.Context, fn func(FlagTx) error) error
}

type FlagTx interface {
    CreateFlag(ctx context.Context, f NewFlag) (Flag, error)                 // ErrAlreadyExists
    UpdateFlagMeta(ctx context.Context, project, key string, m FlagMeta) (Flag, error)
    ArchiveFlag(ctx context.Context, project, key string, at time.Time) error
    RestoreFlag(ctx context.Context, project, key string) error
    SetEnvState(ctx context.Context, project, key, env string, s EnvStateChange,
        expectedVersion int64) (EnvState, error)                              // ErrConflict on mismatch
    AppendAudit(ctx context.Context, e AuditEntry) error                      // no update/delete exists
    BumpRevision(ctx context.Context, project string) (int64, error)
}

type AuthStore interface {
    CreateUser(ctx context.Context, u NewUser) (User, error)
    GetUserByName(ctx context.Context, username string) (User, error)
    CountUsers(ctx context.Context) (int, error)
    DisableUser(ctx context.Context, id string, at time.Time) error
    CreateSession(ctx context.Context, s Session) error
    GetSession(ctx context.Context, tokenHash string) (Session, error)
    TouchSession(ctx context.Context, tokenHash string, at time.Time) error
    DeleteSession(ctx context.Context, tokenHash string) error
    RecordLoginFailure(ctx context.Context, key string, at time.Time) (LoginAttempts, error)
    ResetLoginFailures(ctx context.Context, key string) error
    GetLoginAttempts(ctx context.Context, key string) (LoginAttempts, error)
    CreateAPIToken(ctx context.Context, t NewAPIToken) (APIToken, error)
    GetAPITokenByHash(ctx context.Context, tokenHash string) (APIToken, error)
    TouchAPIToken(ctx context.Context, id string, at time.Time) error
    RevokeAPIToken(ctx context.Context, id string, at time.Time) error
}

type Clock interface{ Now() time.Time }

type Metrics interface {
    Counter(name string, labels map[string]string, delta int64)
    Gauge(name string, labels map[string]string, value float64)
    Histogram(name string, labels map[string]string, value float64)
}
```

Every write use case runs its write, its audit entry and the revision bump in ONE `InTx`. Every
store (`postgres`, `sqlite`, `memory`) passes `storetest.Run`.

## 5. Database schema

All objects are prefixed `jollyroger_`. With `WithSchema("x")` on PostgreSQL they live in schema
`x` instead. Both dialects carry the same tables and columns.

| Table | Columns (keys) |
|---|---|
| `schema_migrations` | `version` PK, `name`, `checksum`, `applied_at` |
| `projects` | `id` PK, `key` UNIQUE, `name`, `created_at` (the `default` project is seeded) |
| `environments` | `id` PK, `project_id` FK, `key`, `name`, `position`, `created_at`; UNIQUE(`project_id`,`key`); `development`, `staging`, `production` seeded |
| `flags` | `id` PK, `project_id` FK, `key`, `name`, `description`, `kind` (`boolean`), `created_at`, `updated_at`, `archived_at` NULL; UNIQUE(`project_id`,`key`) |
| `flag_env_states` | `flag_id` FK, `environment_id` FK, `enabled`, `config` JSON (rules; `{}` in v0.1), `version`, `updated_at`, `updated_by`; PK(`flag_id`,`environment_id`) |
| `revisions` | `project_id` PK, `revision` BIGINT |
| `audit_log` | `id` PK, `project_id`, `environment_key` NULL, `flag_key` NULL, `actor_id`, `actor_name`, `action`, `before` JSON NULL, `after` JSON NULL, `reason` NULL, `created_at`; index (`project_id`, `created_at` DESC, `id` DESC) |
| `audit_prune_guard` | `id` PK (always 1), `active` BOOLEAN (the prune guard, see below) |
| `users` | `id` PK, `username` UNIQUE, `password_hash`, `role`, `created_at`, `disabled_at` NULL |
| `sessions` | `token_hash` PK, `user_id` FK, `csrf_token`, `created_at`, `last_seen_at`, `expires_at` |
| `login_attempts` | `key` PK (`user:<name>` or `ip:<addr>`), `window_start`, `failures`, `locked_until` NULL |
| `api_tokens` | `id` PK, `name`, `prefix`, `token_hash` UNIQUE, `scope`, `environment_id` NULL, `created_by`, `created_at`, `last_used_at` NULL, `revoked_at` NULL |

- IDs are ULIDs (128 bits: 48-bit millisecond time + 80 random bits), stored as 26-character text
  in both dialects, so they sort by creation time.
- `audit_log` rejects UPDATE always, and rejects DELETE unless a prune is in progress, with a
  trigger in both dialects. The guard is a one-row table `jollyroger_audit_prune_guard(active)`
  that the trigger reads (SQLite triggers cannot read session settings, so a table works for both
  dialects). `jollyroger audit prune --before <date>` is the only removal path: in one transaction
  it sets the guard, deletes rows older than the date, clears the guard, and appends an
  `AUDIT_PRUNED` entry recording the date and the count.
- Timestamps are UTC (`TIMESTAMPTZ` on PostgreSQL, RFC 3339 text with `Z` on SQLite).
- Migration rules (idempotent, additive, checksummed, locked) are in `docs/skills/migrations.md`.

## 6. Domain and evaluation model

- `Flag`: stable `ID`, `Key` (`^[a-z0-9][a-z0-9._-]{0,127}$`, unique per project), `Name` (max 200),
  `Description` (max 2000), `Kind` (`boolean`), timestamps, `Archived`.
- `EnvState` (per flag and environment): `Enabled`, `Config`, `Version` (starts at 1, +1 per change),
  `UpdatedAt`, `UpdatedBy`.
- `Config` is built for growth: `{ "rules": [ {"conditions": [...], "serve": Serve} ],
  "fallthrough": Serve }`, where `Serve` is `{"value": bool}` or `{"split": [{"value": bool,
  "weight": int}], "bucket_by": "user_id"|"attr:<name>", "salt": string}`. v0.1 accepts only the
  empty config (no rules, fallthrough `true`); other shapes are rejected by validation until their
  milestone.
- `Context`: `UserID string`, `Attributes map[string]any`.
- `Result`: `Value bool`, `Reason` (`DISABLED`, `STATIC`, `TARGETING_MATCH`, `SPLIT`, `DEFAULT`,
  `ERROR`, aligned with OpenFeature), `FlagVersion int64`, `ErrorCode` (`FLAG_NOT_FOUND`,
  `PARSE_ERROR`, `GENERAL`).

Evaluation algorithm (identical in every SDK):
1. Flag missing or archived in the snapshot: `false`, `ERROR`, `FLAG_NOT_FOUND`.
2. `enabled == false`: `false`, `DISABLED`.
3. Rules in order; the first whose conditions all match serves its `Serve` (`TARGETING_MATCH`, or
   `SPLIT` for a split).
4. Otherwise serve `fallthrough` (`STATIC` for a fixed value, `SPLIT` for a split).
5. A split with no bucketing value available (e.g. empty `UserID`) serves the first variation with
   reason `DEFAULT`. Never random.

Deterministic bucketing (specified now, used from the rollout milestone):
`bucket = uint32 big-endian of the first 4 bytes of SHA-256(flagKey + "." + salt + "." + value) mod
100000`, giving 0.001% granularity. SHA-256 is in every language's standard library.

`protocol/testdata/vectors.json` holds cases `{name, snapshot, flag, context, expected: {value,
reason, error_code}}`. The Go engine runs every case in its tests; every SDK must too.

Snapshot: an immutable, precompiled `map[key]compiledFlag` for one environment plus the revision it
was built from. The hot path is `atomic.Pointer` load, map lookup, precompiled branch: no locks, no
allocation.

## 7. Cache, poller, failure semantics

- The poller calls `Revision(project)` every poll interval (default 5s, +/-10% jitter). Only when
  the revision changed does it call `LoadSnapshot` and swap the pointer.
- A write made through this client swaps its own snapshot immediately after commit (read your own
  writes). Other instances converge within one interval.
- On a refresh error the client keeps the last good snapshot indefinitely, retries with
  exponential backoff capped at 1 minute, logs a rate-limited warning, calls `OnRefreshError`, and
  marks `Status().Stale` once the last success is older than 3 intervals.
- `Status()`: `Revision`, `LastRefresh`, `LastError`, `Stale`, `SchemaVersion`.
- `WithSnapshotFile(path)` writes the last good snapshot atomically (temp file + rename) after each
  change and is read by `New` only when the first load fails.

## 8. HTTP API v1

Under the base path; JSON; errors are RFC 9457 problem details (`docs/skills/error-handling.md`).

| Method | Path | Notes |
|---|---|---|
| GET | `/api/v1/environments` | list |
| POST | `/api/v1/environments` | admin only; `{key, name}`; creates disabled states for every flag |
| GET | `/api/v1/flags?environment=&cursor=&limit=` | keyset-paginated list with the requested environment's state |
| POST | `/api/v1/flags` | `{key, name, description}`; creates disabled states in every environment |
| GET | `/api/v1/flags/{key}` | flag + all environment states; `ETag` per state version |
| PATCH | `/api/v1/flags/{key}` | `{name?, description?}` |
| PUT | `/api/v1/flags/{key}/environments/{env}` | `{enabled, reason?}` + `If-Match: <version>`; 412 on mismatch |
| DELETE | `/api/v1/flags/{key}` | archive |
| POST | `/api/v1/flags/{key}/restore` | unarchive |
| GET | `/api/v1/audit?flag=&environment=&cursor=&limit=` | keyset-paginated, newest first |
| GET | `/api/v1/snapshot?environment=` | local-evaluation protocol; `ETag` = revision; 304 on match |
| POST | `/api/v1/evaluate` | `{environment, context, flags?}` server-side evaluation |

`protocol/openapi.yaml` is the contract; every handler test validates its response against it.
Reserved for later: `GET /api/v1/events` (SSE).

## 9. Authentication, authorization, security

- `Handler()` serves the dashboard only with `WithDashboard(mode)`, and the API only with
  `WithDashboard` (session auth) and/or `WithAPITokens()` (bearer tokens). Otherwise 404.
- Auth modes: `HostAuth(func(*http.Request) (Principal, error))` (the host's own auth identifies the
  user; jollyroger uses the returned `Principal{ID, Name, Role}` for RBAC and audit),
  `BuiltinAuth()` (users in `jollyroger_users`), and `InsecureNoAuth()` (loopback only, for demos).
- Built-in auth: argon2id; 256-bit random session tokens stored as SHA-256; rotation on login;
  30 min idle and 12 h absolute expiry; cookie `jr_session` (HttpOnly, Secure by default,
  SameSite=Strict, Path = base path). First admin via `jollyroger users create` or
  `WithBootstrapAdmin(username, password)`, which acts only when no user exists.
- Roles: `viewer` (read), `editor` (flags), `admin` (users, tokens, environments). Checked in the
  controller for every mutation.
- API tokens: `jr_` + 32 random bytes (base32), shown once, stored as SHA-256, `read` or `write`,
  optional environment scope, revocable, audited.
- CSRF: synchronizer token + `Origin`/`Sec-Fetch-Site` check for cookie sessions.
- Brute force: per-username and per-IP counters in the database with exponential lockout;
  constant-time comparison; dummy hash for unknown users.
- Output: `html/template` only; strict CSP with a per-request nonce; htmx embedded; security
  headers on jollyroger routes only.
- The full rule set is `docs/skills/secure-by-design.md` and `docs/skills/api-security.md`.

## 10. Observability and data

jollyroger adds no destination and requires no vendor (`docs/skills/observability.md`):
- Logs through the host's `*slog.Logger` (`WithLogger`, default the host's `slog.Default()`
  captured once in `New`); nothing logged per evaluation.
- `Metrics` port (no-op default) and per-flag atomic evaluation counters read via `Stats()`;
  `contrib/prometheus` and `contrib/otel` adapt them. Names are fixed in `protocol/metrics.md`.
- `Hooks`: `OnRefresh`, `OnRefreshError`, `OnFlagChanged`, `OnAuthEvent` (Sentry, Slack, anything).
- Spans only around I/O, in `contrib/otel`.
- Data lives only in the host database (`jollyroger_*`). No files by default; evaluations are never
  persisted. Backups and retention are the host database's.

## 11. CLI and Docker (M8)

`jollyroger` binary: `migrate [--print-sql --dialect]`, `flags list|enable|disable|create`,
`users create|disable`, `tokens create|revoke`, `audit prune --before`, `serve` (dashboard + API
against `--db`, for non-Go hosts and demos). It reads configuration from flags and environment
variables (`JOLLYROGER_DATABASE_URL`, ...), and it is the only place env vars are read. A
distroless Docker image runs `serve`.

## 12. Roadmap

| Milestone | Scope |
|---|---|
| M1 Core | `model`, `logic/eval`, `logic/validate`, vectors, benchmarks |
| M2 Storage | ports, `wire/db`, `adapter/db`, migrations, postgres + sqlite + memory, `storetest`, audit trigger, revision |
| M3 Client | root facade, cache, poller, failure semantics, `jollyrogertest`, multi-instance test |
| M4 Controllers + audit | use cases, optimistic concurrency, archive/restore, keyset pagination |
| M5 Auth | host auth, built-in users, sessions, CSRF, lockout, RBAC, tokens |
| M6 HTTP API v1 | `wire/in`/`out`, `adapter/api`, handlers, error registry, OpenAPI tests |
| M7 Dashboard | `diplomat/ui`, htmx |
| M8 v0.1.0 | CLI, Docker, examples, contrib modules, security harness pass, docs, release |
| Later | rollout, targeting, segments, schedules, variants, LISTEN/NOTIFY, SSE, OpenFeature provider, stale-flag detection, SDKs |

Each milestone gets its own implementation plan, TDD, review and PR.

## 13. Risks

1. An admin surface inside other people's apps: off by default, mandatory auth, security harness.
2. Tables in the host database: prefix or own schema, advisory lock, additive-only migrations.
3. Poll load: one indexed single-row read per instance per interval.
4. Cross-SDK drift: shared vectors, storage read contract with a schema major version.
5. SQLite write contention: rare writes; WAL + `busy_timeout` documented.
6. Router mounting quirks: full-path matching; router examples built in CI.
7. API stability: small public surface; `apidiff` in CI before v1.0.0.
8. Non-Go hosts need the Go binary for the dashboard: stated plainly in the README.
9. Diplomat ceremony vs Go community expectations: all of it under `internal/`.

## 14. Tradeoffs

| Choice | Over | Why |
|---|---|---|
| Embedded library | a flag service | zero infrastructure; costs eventual consistency |
| Polling first | push | works on both databases with no driver-specific code |
| Auto-migrate by default | manual only | first-run experience; opt-out exists |
| Local evaluation everywhere | remote evaluation | speed and resilience; needs a precise spec |
| `database/sql` + hand-written SQL | an ORM | fewer dependencies; two explicit dialects |
| htmx + `html/template` | an SPA | no Node toolchain; one binary; easy strict CSP |
| Archive | hard delete | undo; audit context |
| SHA-256 bucketing | murmur3 | in every standard library |
| Full Diplomat layering | flat packages | enforced boundaries; cheap exhaustive tests |
