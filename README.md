# jollyroger

**Feature flags without another server to maintain.**

jollyroger is an open-source feature-flag library for Go that lives *inside* your application:

- flags are stored in the **database you already have** (PostgreSQL or SQLite);
- evaluation happens **in memory**, in your process, with zero allocations and no network call;
- an **opt-in, authenticated dashboard** mounts on your own router;
- every change is recorded in an **immutable audit log**;
- there is **no Redis, no Kafka, no SaaS, and no extra service** to deploy.

> **Status: pre-alpha, design phase.** The API below is the planned developer experience, not
> something you can install yet. Follow the repository for the first release (v0.1.0). The design
> is in [`docs/superpowers/specs`](docs/superpowers/specs), the roadmap is below.

## The planned developer experience

```go
flags, err := jollyroger.New(ctx,
    jollyroger.WithPostgres(db),              // your existing *sql.DB
    jollyroger.WithEnvironment("production"),
    jollyroger.WithDashboard(jollyroger.HostAuth(yourAuthFunc)),
)
if err != nil {
    return err
}
defer flags.Close()

mux.Handle("/__flags/", flags.Handler()) // net/http, chi, gin, echo

if flags.Enabled("new-checkout") {
    newCheckout()
}

flags.Enabled("new-checkout", jollyroger.Context{
    UserID:     "123",
    Attributes: map[string]any{"country": "BR", "plan": "premium"},
})
```

## How it works

```
your application
 ├── your code ── flags.Enabled("x") ── in-memory snapshot (atomic, lock-free)
 │                                          ▲
 │                                          │ reloads only when the revision changes
 ├── jollyroger ── poller ──────────────────┘
 │     └── dashboard + API (opt-in, behind your auth)
 └── your database  (jollyroger_* tables)
```

- **Multiple instances** stay in sync by polling one tiny revision row (push via PostgreSQL
  LISTEN/NOTIFY is planned). No extra infrastructure.
- **If the database goes down**, evaluation keeps serving the last good snapshot and reports
  staleness through `Status()`, your logs, and your metrics.
- **Observability plugs into what you already use**: your `slog` logger, and optional adapters for
  Prometheus and OpenTelemetry. No vendor is required.
- **Other languages** are planned as small evaluation SDKs that read the same tables and pass the
  same shared test vectors (Java first, which also covers Clojure and Kotlin).

## Roadmap

| Milestone | Scope |
|---|---|
| M1 | evaluation engine, domain model, shared test vectors, benchmarks |
| M2 | storage: PostgreSQL, SQLite, in-memory; migrations; conformance suite |
| M3 | Go client: cache, poller, failure semantics, test helper |
| M4 | admin use cases, optimistic concurrency, audit log |
| M5 | authentication: host auth, built-in users, CSRF, lockout, roles, API tokens |
| M6 | HTTP API v1 + OpenAPI contract |
| M7 | dashboard |
| M8 | CLI, Docker image, examples, Prometheus/OpenTelemetry adapters, **v0.1.0** |
| later | percentage rollout, targeting, segments, schedules, variants, LISTEN/NOTIFY, SSE, OpenFeature provider, SDKs for other languages |

## Supported Go versions

The last two Go releases. `go.mod` names the older one.

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md); the engineering rules live in
[CLAUDE.md](CLAUDE.md) and [`docs/skills`](docs/skills).

## Security

Please report vulnerabilities privately, see [SECURITY.md](SECURITY.md).

## License

[Apache License 2.0](LICENSE).

*The name: in One Piece every crew flies its own Jolly Roger. A pirate flag is a feature flag.*
