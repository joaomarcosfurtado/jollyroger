---
description: Turn a rough idea into a reviewed GitHub issue (ready for /ticket).
argument-hint: "<rough idea for the feature or fix>"
allowed-tools: Bash(gh issue create:*), Bash(gh issue view:*), Bash(gh label list:*), Read, Glob, Grep, AskUserQuestion, Skill
---

You are running jollyroger's **`/create-ticket`** command.

First read `docs/skills/ticket-sdd.md` and follow its `/create-ticket` section. `CLAUDE.md` is the
constitution.

Rough idea: **$ARGUMENTS** (if empty, ask for the idea first).

## Do exactly this
1. **Clarify only if ambiguous.** At most two `AskUserQuestion` questions (outcome, public API
   impact, or what "done" means).
2. **Draft the issue body** and show it (GATE 0): Outcome, Definition of Done checklist, Out of
   scope, Public API impact (none / additive / breaking), Protocol impact (OpenAPI, evaluation
   vectors, storage contract). Wait for approval.
3. **Create the issue** with existing labels only (`gh label list`), then print the URL and tell the
   user they can run `/ticket <url>`.

## Guardrails
- Creating an issue is outward-facing: only after GATE 0 approval.
- No spec, plan or code here. This command stops at a created issue.
- No DoD item that cannot be tested.
