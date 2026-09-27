# Skill: Secure by Design

**Read when:** touching auth, sessions, cookies, input decoding, SQL, templates, response headers,
secrets, dependencies, or any new data-access path.

jollyroger mounts an admin surface **inside other people's applications**. A vulnerability here is a
vulnerability in every host that adopts it. Security is a default, not a feature.

## Rules

### Secure defaults (fail closed)
- **MUST** keep the dashboard and HTTP API OFF unless the host calls `WithDashboard(...)` /
  `WithAPI(...)`.
- **MUST** refuse to build a dashboard without an auth mode (`HostAuth`, `BuiltinAuth`). The only
  auth-less mode, `InsecureNoAuth()`, answers 403 to any non-loopback remote address and logs a
  warning at startup.
- **MUST** treat a missing or unparseable required input as an error (400/422), never as a silent
  zero value. Decode JSON with `DisallowUnknownFields` and a size limit (`http.MaxBytesReader`).
- **MUST** ship any control that could lock hosts out (new header, stricter cookie) with a documented
  default and a documented opt-out, and say so in the CHANGELOG.

### Input and SQL
- **MUST** validate every `wire/in` struct with its `Validate()` before it reaches an adapter.
- **MUST** bind every value as a query parameter (`$1` / `?`). **MUST NOT** build SQL with
  `fmt.Sprintf` or `+` from anything that is not a compile-time constant. Table and schema names come
  from constants or from `WithSchema`, which is validated against `^[a-z_][a-z0-9_]{0,62}$` at
  construction.
- **MUST** validate flag keys against `^[a-z0-9][a-z0-9._-]{0,127}$` and cap every free-text field
  (name 200, description 2000, reason 500 characters).

### Output
- **MUST** render HTML only through `html/template` (contextual auto-escaping). **MUST NOT** use
  `template.HTML`, `template.JS` or `template.URL` on data that did not come from our own constants.
- **MUST** serve assets only from `embed.FS` via `fs.Sub`; never join a request path onto a
  filesystem path.
- **MUST** set on every jollyroger response: a strict CSP with a per-request nonce
  (`default-src 'none'; script-src 'nonce-...'; style-src 'self'; img-src 'self'; form-action 'self';
  frame-ancestors 'none'; base-uri 'none'`), `X-Content-Type-Options: nosniff`,
  `Referrer-Policy: no-referrer`, `Cache-Control: no-store` on authenticated pages. Headers apply to
  our routes only; never to the host's.
- **MUST NOT** use inline event handlers (`onclick`, `hx-on:`) or inline scripts without the nonce.

### Authentication and sessions (built-in auth mode)
- **MUST** hash passwords with argon2id (PHC string: memory 64 MiB, time 3, parallelism 2 minimum;
  parameters stored in the hash so they can be raised later). Never plaintext, never a fast hash.
- **MUST** compare secrets with `crypto/subtle.ConstantTimeCompare`, and hash a dummy password when
  the user does not exist, so timing does not reveal valid usernames.
- **MUST** generate session and API tokens from `crypto/rand` (32 bytes) and store ONLY their
  SHA-256. The plaintext token exists in the cookie or is shown once at creation, nowhere else.
- **MUST** rotate the session token on login and privilege change; delete the row on logout.
- **MUST** enforce an idle timeout and an absolute lifetime server-side (defaults 30 min / 12 h).
- **MUST** set the session cookie `HttpOnly`, `Secure` (default on; opt-out only for local HTTP
  development), `SameSite=Strict`, `Path=<base path>`.
- **MUST** rate-limit login per username AND per client IP in the database (so it works across
  instances), with exponential lockout, and audit every failure and lockout.
- **MUST** resolve the client IP from `RemoteAddr` unless the host configured `WithTrustedProxies`;
  never trust `X-Forwarded-For` by default.

### Authorization
- **MUST** check the role in the controller for every mutation (`viewer` < `editor` < `admin`), not
  only in middleware. Defense in depth: the middleware proves who you are, the controller proves you
  may do this.
- **MUST** audit every administrative mutation and every auth event (login ok/failed, lockout,
  user/token created or revoked), in the same transaction as the change.

### CSRF
- **MUST** protect every state-changing dashboard/API request made with a cookie session: a
  per-session synchronizer token (form field or `X-CSRF-Token` header for htmx) AND an
  `Origin` / `Sec-Fetch-Site` check. Token-authenticated API requests (no cookie) are exempt.

### Secrets and configuration
- **MUST NOT** render, log or return DSNs, configuration, password hashes, session tokens, or API
  tokens (after creation).
- **MUST NOT** read environment variables outside `cmd/` (CLAUDE.md rule 1). The library receives
  configuration through options.

### Dependencies (supply chain)
- **MUST** keep the core module to stdlib + `golang.org/x/crypto`. Justify any addition in the PR.
- **MUST** verify a new dependency's module path against its canonical publisher before adding it
  (typo-squatted and AI-hallucinated module names are a real attack vector).
- **MUST NOT** depend on a pre-release version (`-rc`, `-beta`, pseudo-versions of unreleased code)
  except as a documented, time-boxed CVE mitigation.
- **MUST** keep `govulncheck ./...` green (CI runs it on every PR and weekly).

## Principles

| Principle | Here |
|---|---|
| Defense in depth | middleware auth AND controller role check; CSRF token AND Origin check AND SameSite |
| Least privilege | viewer role by default for new users; API tokens scoped read/write and per environment |
| Fail closed | no auth mode means no dashboard; invalid input means 4xx, not a zero value |
| Minimize surface | off by default; one handler under one base path; no CDN, no external calls |
| No obscurity | a hidden path is not access control; `/__flags` is a default, not a secret |

## Related
- [api-security](api-security.md) · [security-review](security-review.md) ·
  [exploratory-security](exploratory-security.md) · [error-handling](error-handling.md) ·
  [logging](logging.md)
