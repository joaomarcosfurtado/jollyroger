# Skill: Exploratory Security (attack it like an adversary)

**Read when:** doing a security pass on a feature before release, or after any change to auth,
sessions, CSRF, the dashboard, or the API. [secure-by-design](secure-by-design.md) says WHAT must
hold; this skill is HOW to prove it holds on the running handler.

## Rules
- **MUST** use both lenses: (1) live attacks through the real `http.Handler` built by
  `jollyroger.New`, against real PostgreSQL and SQLite; (2) a static read of every controller the
  feature touches, looking for authorization checks the happy path never exercises.
- **MUST** fire a CONTROL request first for every attack: the same request made legitimately must
  succeed. A 404 from a wrong method or path reads as "the guard held" when the route never ran.
- **MUST** run the full matrix below for every new endpoint (the harness enumerates routes from the
  router, so a new route without a matrix entry fails).
- **MUST** fix every P0/P1 with a fail-before/pass-after test (unit where the logic lives, plus a
  harness case) before release.
- **MUST** record each pass in `docs/security/<feature>-findings.md`: Fixed, Tooling gaps, Accepted
  (with reason), Confirmed correct.

## The harness: `test/security` (build tag `integration`)
- Boots the root package with each auth mode (host auth, built-in, token) on each store.
- Seeds two principals per role (`viewer`, `editor`, `admin`) so privilege escalation is provable.
- Provides primitives: `AuthGuard`, `RoleEscalation`, `CSRFMissingToken`, `CSRFCrossOrigin`,
  `SQLiProbe`, `XSSProbe`, `BruteForce`, `SessionFixation`, `TokenScope`, `PathTraversal`,
  `SecretScan`, `HeaderCheck`.
- Walks the router's route table so every route is attacked by default; exemptions are a named,
  commented list in the harness, never silent.

## The matrix

| Concern | Attack | Secure outcome |
|---|---|---|
| Auth guard | anonymous request to every dashboard/API route | 401 (API) or redirect to login (dashboard) |
| Default off | same requests with `WithDashboard` not set | 404, handler never runs |
| Role escalation | viewer calls editor/admin mutations; editor calls admin | 403/404, state unchanged in the DB |
| Token scope | read token mutates; env-scoped token touches another env | 403, state unchanged |
| CSRF | cookie session mutation without token; with token but `Origin: evil` | 403, state unchanged |
| Brute force | N wrong passwords for one user; spread across users from one IP | lockout + 429, audit rows written |
| Enumeration | login with unknown vs known user | same status, body and timing envelope |
| Session | fixation (pre-set cookie survives login?), reuse after logout, expiry | rotated, rejected, rejected |
| SQLi | `' OR 1=1--`, `"; DROP`, unicode in keys, names, cursors, query params | 4xx, never 500, tables intact |
| XSS | `<script>`, `"><img onerror>` in flag name/description/reason/actor name | escaped in every rendered page |
| Path traversal | `../`, `%2e%2e%2f`, encoded backslashes on the asset route | 404, no file outside `embed.FS` |
| Secrets | scan every response body and log line produced during the pass | no DSN, hash, token, stack trace |
| Headers | every response | CSP with nonce, nosniff, frame-ancestors none, no-store |

## Triage before calling it a bug
- A 2xx is a finding only if the state changed or data leaked: check the database row.
- A 500 on a probe is always a finding (at minimum P2: an unhandled input path).

## Severity
- **P0** live exploit: unauthenticated or cross-role mutation, SQLi, secret in a body.
- **P1** missing control: unguarded route, CSRF gap, unescaped output, no lockout.
- **P2** information leak: user enumeration, 403-vs-404 existence leak, verbose error.
- **P3** hardening recommendation.

## Related
- [secure-by-design](secure-by-design.md) · [security-review](security-review.md) ·
  [api-security](api-security.md) · [testing-standards](testing-standards.md)
