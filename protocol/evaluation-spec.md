# Evaluation specification (version 1)

This document defines how a jollyroger SDK decodes a snapshot and evaluates a flag. The key words
MUST, MUST NOT and SHOULD are used as in RFC 2119. Every SDK MUST pass `testdata/vectors.json`.

## 1. Snapshot document

A snapshot is the complete evaluation data for one environment at one revision:

```json
{
  "schema_version": 1,
  "environment": "production",
  "revision": 42,
  "flags": [
    {"key": "new-checkout", "enabled": true, "version": 3,
     "config": {"rules": [], "fallthrough": {"value": true}}}
  ]
}
```

Shapes:
- `config` is `{"rules": [Rule], "fallthrough": Serve}`.
- `Rule` is `{"conditions": [Condition], "serve": Serve}`; `Condition` is
  `{"attribute": string, "operator": string, "values": [string]}`.
- `Serve` is `{"value": bool}`, `{"split": Split}`, or `{}` (serve `true`). Setting both `value`
  and `split` is invalid.
- `Split` is `{"variations": [{"value": bool, "weight": int}], "bucket_by": "user_id" |
  "attr:<name>", "salt": string}`, with weights in units of 0.001% summing to 100000.
- Archived flags are never present in a snapshot.

## 2. Decoding rules

These rules exist so that every language decodes the same document the same way.

1. Field names are case-sensitive: `"ENABLED"` is an unknown field, not `"enabled"`.
2. `null` means absent, at every level. An absent `config`, `rules`, `fallthrough`, `value` or
   `split` takes its empty form, so an enabled flag with no config serves `true`.
3. At the document and flag level, a reader MUST ignore unknown fields (newer writers may add
   fields within a schema version).
4. Inside a `config` (the config object and every Rule, Condition, Serve, Split and variation in
   it), any unknown field means "a feature this reader does not implement". A wrongly typed
   member, or a config that is not an object, is invalid. Either way the reader MUST treat that
   flag's config as unusable (section 3, step 5) and MUST keep decoding the other flags.
5. A writer that holds a config it cannot represent MUST emit `{"unparseable": true}` as that
   flag's config. By rule 4 every reader treats it as unusable.
6. `schema_version` MUST be an integer literal (`1`, not `1.0` or `"1"`). A reader MUST reject a
   document whose `schema_version` is missing or not supported.
7. A wrongly typed document-level or flag-level field (`schema_version`, `environment`,
   `revision`, `flags`, `key`, `enabled`, `version`), or a document that is not an object, makes
   the whole document invalid; the reader MUST reject it (an SDK keeps its last good snapshot).
8. If an object repeats a field name, the last occurrence wins.

## 3. Algorithm

Given a snapshot, a flag key, and a context `{user_id, attributes}`:

1. If no snapshot is loaded yet: `value=false`, `reason=ERROR`,
   `error_code=PROVIDER_NOT_READY`.
2. If the key appears more than once in the snapshot: `value=false`, `reason=ERROR`,
   `error_code=PARSE_ERROR`.
3. If the key is absent (keys are case-sensitive): `value=false`, `reason=ERROR`,
   `error_code=FLAG_NOT_FOUND`.
4. If `enabled` is false: `value=false`, `reason=DISABLED`, whatever the configuration.
5. If the configuration is unusable (section 2, rules 4 and 5), invalid, or uses a feature this
   SDK does not implement: `value=false`, `reason=ERROR`, `error_code=PARSE_ERROR`. Other flags
   MUST be unaffected.
6. Rules, in order (version 1 SDKs implement none and apply step 5 to any rule).
7. Otherwise serve `fallthrough`: a fixed value gives `reason=STATIC`; `{}` gives `value=true`,
   `reason=STATIC`.

Every result carries the flag's `version` as `flag_version` (0 when the flag is absent or its
key is duplicated). An SDK MUST NOT throw or panic on evaluation, and SHOULD NOT allocate or
block.

## 4. Bucketing

For percentage splits (specified now, evaluated from a later version):

```
bucket = uint32_big_endian(SHA-256(utf8(flag_key + "." + salt + "." + value))[0:4]) mod 100000
```

`value` is the context's `user_id` when `bucket_by` is `user_id`, or the attribute `<name>` for
`attr:<name>`. Only string attributes can be bucketed: a missing attribute, or one whose value is
not a string (numbers format differently across languages), counts as missing. A split whose
bucketing value is empty or missing serves its first variation with `reason=DEFAULT`; it is never
random. `bucket_cases` in the vectors pin this function.

## 5. Vectors file

`testdata/vectors.json` contains:
- `vectors_version`;
- `evaluation_cases`: a snapshot document, a `flag`, a `context`, and `expected`
  `{value, reason, error_code, flag_version}`, where an empty `error_code` means none;
- `bucket_cases`: `flag_key`, `salt`, `value`, `expected`;
- `document_cases`: a `document` and `expect`, either `accept` or `reject` (section 2).
