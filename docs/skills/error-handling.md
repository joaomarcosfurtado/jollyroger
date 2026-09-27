# Skill: Error Handling

**Read when:** returning an error from a controller, mapping an error to HTTP, or adding an error
code.

There is ONE source of truth for how a failure reaches a caller: the error registry in
`internal/diplomat/httpserver`. Controllers speak in sentinel errors; the registry decides status,
problem type, and message.

## Rules
- **MUST** express every expected business failure as a sentinel error declared in
  `internal/model/errors.go` (`ErrNotFound`, `ErrConflict`, `ErrAlreadyExists`, `ErrInvalid`,
  `ErrForbidden`, `ErrUnauthenticated`, `ErrLocked`, ...), wrapped with context using
  `fmt.Errorf("set flag state %q: %w", key, model.ErrConflict)`.
- **MUST** carry field-level validation details in `*model.ValidationError` (which `errors.Is`
  `model.ErrInvalid`) so the API can return per-field messages.
- **MUST** map errors to responses ONLY through the registry (`errorRegistry` in
  `httpserver/errors.go`). **MUST NOT** write a `switch` on errors inside a handler.
- **MUST**, when adding a sentinel, add its registry row AND extend the registry-completeness test
  that asserts every sentinel in `model` has a row.
- **MUST** return RFC 9457 problem details (`application/problem+json`) from the JSON API and the
  matching error fragment from the dashboard. Never an HTML page to a JSON client.
- **MUST NOT** leak internals in a response: no raw `err.Error()` of an unexpected error, no SQL, no
  stack, no DSN. Unknown errors become a generic 500 with a request id; the detail goes to the log.
- **MUST NOT** panic for an expected failure. A panic in a handler is recovered by the server
  middleware, logged at error level, and answered with the generic 500.
- **MUST** use `errors.Is` / `errors.As`, never string comparison of error messages.
- **MUST** treat "not yours" and "does not exist" the same way (404 `ErrNotFound`) wherever
  revealing existence would leak information.

## The registry

| Sentinel | HTTP | Problem type suffix | When |
|---|---|---|---|
| `ErrInvalid` / `*ValidationError` | 422 | `validation` | well-formed input that breaks a rule (bad key, too long) |
| (decode failure in handler) | 400 | `bad-request` | malformed JSON, wrong types, unknown fields |
| `ErrUnauthenticated` | 401 | `unauthenticated` | no or invalid session/token |
| `ErrForbidden` | 403 | `forbidden` | authenticated but role too low (only where existence is not secret) |
| `ErrNotFound` | 404 | `not-found` | missing, archived where not allowed, or not visible to the caller |
| `ErrAlreadyExists` | 409 | `already-exists` | duplicate flag key |
| `ErrConflict` | 412 on `If-Match`, else 409 | `conflict` | optimistic-concurrency version mismatch |
| `ErrLocked` | 429 | `locked` | login lockout / rate limit (with `Retry-After`) |
| anything else | 500 | `internal` | unexpected; logged with request id, generic body |

Problem `type` URIs are `https://jollyroger.dev/problems/<suffix>` (documented in
`protocol/openapi.yaml`); clients switch on `type`, never on `title` text.

## Why
Inline `switch` blocks in handlers drift: one handler returns 409 for a conflict, another 400, and
the error surface becomes untestable. One registry makes the mapping reviewable in one file and
lets a single test prove every sentinel is mapped.

## Related
- [api-security](api-security.md) · [diplomat-architecture](diplomat-architecture.md) ·
  [logging](logging.md)
