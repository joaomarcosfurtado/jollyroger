# Skill: Observability (metrics, traces, hooks, health)

**Read when:** adding a metric, a span, a hook, a health signal, or a `contrib/` adapter. Logging
itself is owned by [logging](logging.md).

The principle: **jollyroger adds no new destination and requires no vendor.** It reports into
whatever the host already runs (Prometheus, OpenTelemetry, Datadog, StatsD, Sentry, plain logs, or
nothing) through stdlib-typed extension points.

## Rules
- **MUST NOT** import any observability library (OpenTelemetry, Prometheus, Sentry, Datadog, ...)
  from the core module. Vendor integrations live in `contrib/<name>/` as separate Go modules.
- **MUST** expose observability only through these extension points, all optional:
  1. `WithLogger(*slog.Logger)` (see [logging](logging.md));
  2. `WithMetrics(Metrics)`: the `Metrics` port (counters, gauges, histograms with string labels),
     default no-op;
  3. `WithHooks(Hooks)`: a struct of optional funcs (`OnRefresh`, `OnRefreshError`,
     `OnFlagChanged`, `OnAuthEvent`);
  4. `Client.Status()` and `Client.Stats()`: plain structs the host can export anywhere.
- **MUST** keep the evaluation hot path free of calls into `Metrics` or `Hooks`. Count evaluations in
  per-flag, per-reason atomic counters owned by the client; `Stats()` and the contrib collectors
  read them on scrape.
- **MUST** call hooks outside any lock and outside the store transaction, recover a panicking hook
  (log at error, never crash the host), and never block the caller on a slow hook for longer than
  documented (hooks run synchronously on the poller/admin goroutine; hosts that need async do it
  inside the hook).
- **MUST** keep metric labels low-cardinality and fixed: `flag` (bounded by the number of flags),
  `reason`, `environment`, `result`. **MUST NOT** use user IDs, attribute values, or request paths
  as labels.
- **MUST** use the names defined in `protocol/metrics.md` (shared by every SDK):
  `feature_flag_evaluations_total{flag,reason}`, `feature_flag_evaluation_errors_total{flag,code}`,
  `feature_flag_snapshot_refreshes_total{result}`, `feature_flag_snapshot_age_seconds`,
  `feature_flag_snapshot_revision`, `feature_flag_updates_total{environment,action}`.
- **MUST** create spans only around I/O (snapshot load, migrations, admin writes, dashboard/API
  requests), never around an evaluation. Span and attribute names follow the OpenTelemetry feature
  flag semantic conventions where they exist.
- **MUST** make every instrument fail-safe: an exporter error or panic never propagates into the host.
- **MUST** document any new extension point or metric in `docs/observability.md` (user-facing) with
  a copy-paste example.

## Health contract
`Client.Status()` returns `{Revision, LastRefresh, LastError, Stale, SchemaVersion}`. `Stale` is true
when the last successful refresh is older than `3 x poll interval`. `HealthHandler()` is a
dependency-free `http.Handler` answering 200/503 from `Status()`, for hosts that want it; most will
fold `Status()` into their own health endpoint.

## contrib modules (conveniences, not requirements)

| Module | Implements | Uses the host's |
|---|---|---|
| `contrib/prometheus` | a `prometheus.Collector` over `Stats()` + the `Metrics` port | `prometheus.Registerer` |
| `contrib/otel` | `Metrics` port on a `metric.Meter`; span helpers | `MeterProvider` / `TracerProvider` |

Anything else (Datadog, StatsD, New Relic, Sentry via `OnRefreshError`) is a 20-line adapter the
host writes, shown in `docs/observability.md`.

## Related
- [logging](logging.md) · [diplomat-architecture](diplomat-architecture.md) ·
  [secure-by-design](secure-by-design.md)
