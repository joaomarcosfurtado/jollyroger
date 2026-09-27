# Skill: Writing Skills (agent-optimized format)

**Read when:** creating or editing any file in `docs/skills/`.

## Rules
- **MUST** open every skill with the H1, a one-line `**Read when:**`, then a `## Rules` block: a
  scannable list of MUST / MUST NOT / SHOULD bullets an agent can turn into TODOs.
- **MUST** make each rule imperative and concrete: name the package, file, function or command it
  governs.
- **MUST** put reference material (tables, examples, rationale, incident stories) BELOW the Rules.
- **MUST** keep one skill to one job; split a skill that covers two concerns.
- **MUST NOT** restate what another skill owns; link to it (`[name](name.md)`).
- **MUST** add a new skill to the skills table in `CLAUDE.md` in the same change.
- **MUST NOT** use the em-dash character (CLAUDE.md rule 15).
- **SHOULD** prefer tables and checklists over prose; rules buried in paragraphs get missed.
- **SHOULD** keep a skill under about 150 lines; if it grows, cut prose or split it.
- **SHOULD** record the *why* of a rule as a short incident story ("this broke because..."). A rule
  without its reason gets deleted by the next person who finds it inconvenient.

## Why
Agents and busy humans skim. A rules-first block is the contract; the body is the explanation you
read only when you need detail. Rules placed last, or written as prose, are the rules that get
missed.

## Template
```md
# Skill: <Name>

**Read when:** <one line>

## Rules
- **MUST** <do X, in package/file Y>
- **MUST NOT** <do Z>

<reference: tables, examples, rationale>

## Related
- [other-skill](other-skill.md)
```

## Related
- [documentation](documentation.md)
