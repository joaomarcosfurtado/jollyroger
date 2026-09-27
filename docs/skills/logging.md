# Skill: Logging

**Read when:** adding a log line anywhere in the library. Metrics, traces, hooks and health are in
[observability](observability.md).

jollyroger is a guest in the host's process. Its logs must land in the host's stream, in the host's
format, at a volume the host never notices.

## Rules
- **MUST** log only through the `*slog.Logger` threaded into the component (from `WithLogger`, or
  the host's `slog.Default()` captured once in the root `New`). **MUST NOT** call `log.Printf`,
  `fmt.Println`, `slog.Info` (package-level) or read `slog.Default()` anywhere else.
- **MUST** derive every component logger with `logger.With("component", "jollyroger",
  "subsystem", "<poller|store|httpserver|auth|...>")` so hosts can filter us out or in.
- **MUST NOT** log anything per flag evaluation. The hot path has zero logging and zero allocations.
  Evaluation visibility comes from counters ([observability](observability.md)).
- **MUST** rate-limit repetitive warnings (unknown flag key, repeated refresh failure) to at most one
  line per key per interval, and include the suppressed count when it resumes.
- **MUST** use structured attributes (`slog.String("flag", key)`), never values interpolated into
  the message. Messages are constant strings, so hosts can group and alert on them.
- **MUST** pass `ctx` (`logger.InfoContext(ctx, ...)`) whenever one exists, so the host's handler
  can attach its request id or trace id.
- **MUST NOT** log secrets or personal data: no passwords, hashes, session or API tokens, DSNs,
  evaluation `Context.Attributes` values, or user IDs from evaluation contexts. Actor names/IDs of
  dashboard users are allowed in audit-related lines (they are already in the audit log).
- **MUST** log the error value as an attribute (`slog.Any("err", err)`) at the point where it is
  handled, not at every layer it passes through (log once, where you decide what to do).

## Levels

| Level | When | Examples |
|---|---|---|
| Debug | detail useful when diagnosing | snapshot reloaded (revision, flag count, duration) |
| Info | rare, meaningful state changes | migrations applied; flag state changed (key, env, actor); user created |
| Warn | handled but unexpected | refresh failed, serving stale snapshot; unknown flag key (rate-limited); login lockout |
| Error | the library cannot do its job | migration failed; snapshot never loaded; recovered panic in a handler |

## What NOT to log
- Anything on the evaluation hot path.
- Request/response bodies.
- Evaluation context attributes (they are the host's user data).
- Secrets of any kind.

## Related
- [observability](observability.md) · [secure-by-design](secure-by-design.md) ·
  [error-handling](error-handling.md)
