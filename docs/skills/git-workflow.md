# Skill: Git Workflow

**Read when:** starting any change: feature, fix, docs, release.

```
worktree + branch  --PR-->  main (protected: CI green + review)  --tag vX.Y.Z-->  release
```

## Rules
- **MUST** work in your OWN git worktree on a named branch, in a sibling directory:
  `git worktree add ../jollyroger-wt/<branch-dir> -b <type>/<kebab-name>`, then
  `git -C ../jollyroger-wt/<branch-dir> push -u origin HEAD` immediately.
- **MUST** use a branch prefix: `feat/`, `fix/`, `docs/`, `chore/`, `refactor/`, `test/`, `perf/`.
  Keep names short (2 to 4 words, kebab-case).
- **MUST** reach `main` only through a pull request with the CI checks green. **MUST NOT** push to
  `main` directly (branch protection enforces it) or self-merge an agent PR without the maintainer's
  explicit go-ahead in chat.
- **MUST** title PRs and squash commits with Conventional Commits (`feat(eval): ...`,
  `fix(store/sqlite): ...`, `docs: ...`, `feat!:` for breaking). The title feeds the CHANGELOG.
- **MUST** add a CHANGELOG entry under `## [Unreleased]` for any user-visible change.
- **MUST** end agent-authored commit messages with
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- **MUST NOT** remove a worktree until the maintainer says the job is done; then remove the
  worktree only. **MUST NOT** delete branches (local or remote) without asking.
- **MUST NOT** put worktrees inside the repo (`.worktrees/`, `.claude/worktrees/`).
- **MUST NOT** commit secrets or `.env*` files; `.gitignore` covers them, gitleaks runs in CI.
- **MUST NOT** rewrite published history (`push --force` to a shared branch, amend a pushed commit).
  Prefer a new commit.

## Per-task recipe
```bash
git -C ../jollyroger fetch origin
git worktree add ../jollyroger-wt/feat-rollout -b feat/percentage-rollout origin/main
git -C ../jollyroger-wt/feat-rollout push -u origin HEAD
cd ../jollyroger-wt/feat-rollout
# baseline the full gate first (CLAUDE.md "Running the gate"), then work test-first
gh pr create --base main --title "feat(eval): percentage rollout" --body "..."
```

## Releases (semver)
- Tags are `vMAJOR.MINOR.PATCH` on `main`; `contrib/<name>` modules are tagged
  `contrib/<name>/vX.Y.Z` (Go multi-module convention).
- Before v1.0.0, a MINOR bump may break the API but MUST be called out under `### Breaking` in the
  CHANGELOG. From v1.0.0, breaking changes need a new major version (`/v2` module path).
- A release PR moves `Unreleased` entries under the new version heading; the tag is pushed after it
  merges. GitHub Releases reuse the CHANGELOG section.

## CI (`.github/workflows/ci.yml`)
Runs on every PR and on push to `main`: build, `go vet`, unit + race tests on the two supported Go
versions, integration tests against PostgreSQL and SQLite, golangci-lint, govulncheck, gitleaks.
`main` branch protection requires these checks. A weekly scheduled run re-checks govulncheck so a
newly disclosed CVE surfaces between releases.

## Related
- [documentation](documentation.md) · [ticket-sdd](ticket-sdd.md) ·
  [local-development](local-development.md)
