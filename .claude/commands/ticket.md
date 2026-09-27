---
description: Spec-driven pipeline, take a GitHub issue to a tested, reviewed PR.
argument-hint: "<github-issue-url>"
allowed-tools: Bash, Read, Write, Edit, Glob, Grep, AskUserQuestion, Skill, TodoWrite
---

You are running jollyroger's **`/ticket`** command.

FIRST read `docs/skills/ticket-sdd.md` and follow its phases and rules. `CLAUDE.md` is the
constitution; obey the skills it links (architecture, testing, security, git workflow).

Issue: **$ARGUMENTS** (if not a GitHub issue URL, ask for one).

## Execute the phases in order, STOPPING at every gate
- **P0 Ingest:** `gh issue view $ARGUMENTS --json title,body,labels,number`; derive the slug.
- **P1 Clarify [GATE 1]:** always ask investigation depth (light / standard / deep), public API
  impact, protocol impact, and any ambiguous DoD item.
- **P2 Spec [GATE 2]:** write `specs/<slug>/spec.md` per the contract; on approval set
  `Status: Approved`.
- **P3 Plan [GATE 3]:** `superpowers:writing-plans` -> `specs/<slug>/plan.md`, every task tagged with
  its DoD row.
- **P4 Implement:** own worktree per `git-workflow.md`, TDD task by task.
- **P5 Verify [GATE 4]:** full gate (build, vet, test, race, integration, golangci-lint,
  govulncheck), `/code-review`, `review-checklist.md`, `security-review.md`; write
  `change-report.md` + `test-pack.md`; fill the DoD matrix.
- **P6 Integrate:** open the PR to `main` with `Closes #<N>`; do not merge without the maintainer.
- **P7 Close-out:** after merge set `spec.md` to `Status: Done`.

## Guardrails
- Never skip or auto-pass a gate.
- Never implement beyond the approved spec, never leave a DoD item uncovered.
- Store changes need PostgreSQL AND SQLite integration tests (CLAUDE.md rule 12).
- Track phases with TodoWrite.
