# Skill: Security Review (pre-merge checklist)

**Read when:** finishing any branch. Run this checklist over the diff before opening or merging the
PR. It complements the architecture checklist in [review-checklist](review-checklist.md).

## Rules
- **MUST** walk every applicable box below before declaring a branch done.
- **MUST NOT** merge with an unchecked box that applies to the change.
- **MUST** explain any intentional exception in the PR description so reviewers see it.

## Checklist

### Defaults
- [ ] Nothing new is reachable without `WithDashboard`/`WithAPI` and an auth mode.
- [ ] New options default to the safe value; any opt-out is documented in the option's doc comment.

### Input
- [ ] New request shapes are `wire/in` structs with a `Validate()` method and a table-driven test.
- [ ] JSON decoding uses the shared decoder (size limit + `DisallowUnknownFields`).
- [ ] Missing required input fails closed with 400/422, never a zero value.

### SQL
- [ ] Every value is a bound parameter; no `fmt.Sprintf` / concatenation of non-constant SQL.
- [ ] Any identifier (schema/table) comes from a constant or the validated `WithSchema` value.

### Output
- [ ] HTML only via `html/template`; no new `template.HTML`/`JS`/`URL` on non-constant data.
- [ ] No inline scripts or handlers without the CSP nonce.
- [ ] Error responses go through the registry; no internal detail in bodies.

### AuthN / AuthZ
- [ ] Every new mutation checks role (and token scope) in the controller, not only middleware.
- [ ] Every new mutation writes an audit entry in the same transaction.
- [ ] Session/token handling uses the existing helpers (hash-at-rest, constant-time compare,
      rotation on login).

### CSRF / headers
- [ ] New state-changing routes sit behind the CSRF middleware (cookie auth).
- [ ] Security headers apply to the new route; no CSP loosening without a justifying comment.

### Secrets and logs
- [ ] No token, hash, password, DSN or config value is logged, rendered or returned.
- [ ] New log lines carry only low-cardinality, non-secret attributes ([logging](logging.md)).

### Dependencies
- [ ] `go.mod` changes: module path verified at the publisher, stable release only, and
      `govulncheck ./...` clean. Core module still stdlib + `golang.org/x/crypto`.

### Tests
- [ ] At least one test proves the security-relevant behaviour (fails before, passes after).
- [ ] New endpoints are covered by the `test/security` harness matrix
      ([exploratory-security](exploratory-security.md)).

## Related
- [secure-by-design](secure-by-design.md) · [api-security](api-security.md) ·
  [review-checklist](review-checklist.md) · [testing-standards](testing-standards.md)
