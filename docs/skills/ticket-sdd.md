# Skill: Ticket SDD Pipeline (issue -> spec -> plan -> code -> review)

**Read when:** running `/create-ticket` or `/ticket`, or maintaining either command. This is the
single source of truth for the spec-driven flow; the commands in `.claude/commands/` stay thin.

## Rules
- **MUST** treat `CLAUDE.md` (rules + engineering bar) as the constitution. A spec that conflicts
  with it is wrong.
- **MUST** keep a ticket's artifacts in `specs/<ticket-slug>/`: `spec.md` (the binding contract),
  `plan.md`, `change-report.md`, `test-pack.md`.
- **MUST** derive `<ticket-slug>` as kebab-case of the issue title and reuse it for the branch
  (`feat|fix/<ticket-slug>`).
- **MUST** stop at every **[GATE]** and wait for the maintainer. Never auto-pass a gate.
- **MUST** implement ONLY what `spec.md` covers: nothing beyond the DoD, no DoD item uncovered.
- **MUST** keep `spec.md` living: when a decision changes, update it and re-confirm at the gate.
- **MUST** run the full gate in P5 and flag every diff hunk that does not trace to a DoD item (the
  scope fence) in `change-report.md`.
- **MUST NOT** restate architecture or security rules here; load the owning skills.

## Phases (`/ticket <issue-url>`)

| Phase | Does | Gate |
|---|---|---|
| P0 Ingest | `gh issue view <url> --json title,body,labels,number`; parse Outcome + DoD | |
| P1 Clarify | targeted questions: investigation depth, public API impact, protocol impact, non-goals | GATE 1 |
| P2 Spec | write `specs/<slug>/spec.md` (contract below) | GATE 2 |
| P3 Plan | `superpowers:writing-plans` -> `specs/<slug>/plan.md`, each task tagged with its DoD row | GATE 3 |
| P4 Implement | own worktree, TDD task by task, obey the skills | |
| P5 Verify | full gate + `/code-review` + [review-checklist](review-checklist.md) + [security-review](security-review.md); write change-report + test-pack | GATE 4 |
| P6 Integrate | open the PR to `main` with `Closes #<N>`; wait for review and CI | |
| P7 Close-out | on merge: `spec.md` Status -> Done; worktree removed only on sign-off | |

## Spec contract (`spec.md`)
1. `Status: Draft | Approved | In Review | Done` on the first line.
2. Outcome and context (link `Issue #<N>`).
3. DoD-coverage matrix.
4. Out-of-scope fence.
5. Constitution check: layers, public API change (semver impact), protocol change (vectors,
   OpenAPI, storage contract), security, integration tests for store code.
6. Prior decisions and constraints from P1.

### DoD-coverage matrix
| # | DoD item | Spec requirement | Plan task(s) | Test(s) | Status (P5) |
|---|---|---|---|---|---|

Every DoD row needs a requirement, a task and a test; no task may exist without a DoD anchor.

## change-report.md and test-pack.md
- change-report: per changed file and hunk, `what / why / DoD item / traceable yes|FLAG`, then a
  scope-fence summary of every FLAG for the maintainer to accept (amend the spec) or reject.
- test-pack: the exact gate commands with their output, plus manual steps per DoD item (which
  example to run, which URL, which expected result).

## `/create-ticket <idea>`
1. Clarify with one or two questions only if the idea is ambiguous.
2. Draft the issue body: Outcome, DoD checklist, Out of scope, public-API / protocol impact. Show it
   (GATE 0).
3. On approval, `gh issue create` with existing labels only; print the URL for `/ticket`.

## Related
- [git-workflow](git-workflow.md) · [testing-standards](testing-standards.md) ·
  [review-checklist](review-checklist.md) · [documentation](documentation.md)
