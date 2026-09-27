## What and why

<!-- One or two sentences. Link the issue: Closes #N -->

## Public API / protocol impact

- [ ] none
- [ ] additive (new exported identifier, endpoint, option, or field)
- [ ] breaking (explained under `### Breaking` in CHANGELOG)
- [ ] protocol files changed (`openapi.yaml`, evaluation vectors, storage contract, metrics)

## Checklist

- [ ] Tests ship with the change; a fix has a test that fails without it
- [ ] Store changes tested on PostgreSQL AND SQLite (`go test -tags integration ./...`)
- [ ] `go vet`, `go test -race`, golangci-lint, govulncheck are green
- [ ] Walked `docs/skills/review-checklist.md` and `docs/skills/security-review.md`
- [ ] Docs and CHANGELOG (`Unreleased`) updated
