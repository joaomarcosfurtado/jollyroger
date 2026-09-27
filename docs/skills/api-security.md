# Skill: API Security (the JSON API boundary)

**Read when:** adding or changing anything under `/api/v1`, API tokens, CORS, rate limits, or the
snapshot / evaluate endpoints consumed by other SDKs. The general rules live in
[secure-by-design](secure-by-design.md); this skill applies them to the JSON boundary.

## Rules
- **MUST** shape every endpoint as: decode into `wire/in` (size-limited, `DisallowUnknownFields`) ->
  `Validate()` -> `adapter/http` -> controller -> `adapter/http` -> `wire/out` -> JSON. Handlers
  hold no business rules.
- **MUST** accept exactly two credentials: the dashboard session cookie (plus CSRF) and an API
  token in `Authorization: Bearer jr_...`. **MUST NOT** accept tokens in query strings (they leak
  into logs, history and referrers).
- **MUST** scope API tokens: `read` or `write`, optionally to one environment, checked in the
  controller. A read token can call `GET` endpoints, `/snapshot` and `/evaluate`, nothing else.
- **MUST** update `last_used_at` for tokens at most once per minute (no write amplification) and
  reject revoked or expired tokens with 401 and a generic body.
- **MUST** keep CORS OFF by default. If a host enables it, it names explicit origins;
  **MUST NOT** combine `Access-Control-Allow-Origin: *` with credentials.
- **MUST** rate-limit authentication failures per token prefix and per IP, and cap request bodies
  (default 64 KiB).
- **MUST** return errors as RFC 9457 problem JSON through the registry
  ([error-handling](error-handling.md)); never an HTML body, stack trace, or raw driver error.
- **MUST** support optimistic concurrency on mutations: `ETag` with the state version on reads,
  `If-Match` on writes, `412` on mismatch. Never silently last-write-wins on a flag state.
- **MUST** version the API in the path (`/api/v1`). A breaking change is `/api/v2`; within v1,
  changes are additive only (new optional fields, new endpoints).
- **MUST** keep `protocol/openapi.yaml` the contract: every handler test validates its response
  against it, and a new endpoint lands in the spec in the same change.
- **MUST** send `Cache-Control: no-store` on every API response except `/snapshot`, which uses
  `ETag` = revision and answers `304` to a matching `If-None-Match`.

## Endpoint auth matrix

| Endpoint | Session (cookie+CSRF) | Read token | Write token |
|---|---|---|---|
| `GET /environments`, `GET /flags`, `GET /flags/{key}`, `GET /audit` | viewer+ | yes | yes |
| `POST/PATCH/DELETE /flags...`, `PUT .../environments/{env}` | editor+ | no | yes (env scope checked) |
| `GET /snapshot`, `POST /evaluate` | viewer+ | yes | yes |
| users / tokens management | admin | no | no |

## The request lifecycle (where each control fires)
```
body size cap -> security headers -> recover -> request id -> authenticate (session | token)
  -> CSRF (session only) -> rate limit -> route -> decode + Validate (wire/in) -> adapter
  -> controller (role + scope re-check, audit in tx) -> adapter -> encode (wire/out)
panic or unknown error -> generic problem JSON 500 with request id (detail in the log only)
```

## Related
- [secure-by-design](secure-by-design.md) · [error-handling](error-handling.md) ·
  [security-review](security-review.md) · [observability](observability.md)
