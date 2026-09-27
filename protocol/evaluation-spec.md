# Evaluation specification (version 1)

This document defines how a jollyroger SDK evaluates a flag. The key words MUST, MUST NOT and
SHOULD are used as in RFC 2119. Every SDK MUST pass `testdata/vectors.json`.

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

- A reader MUST reject a document whose `schema_version` it does not support.
- A reader MUST ignore unknown fields (newer writers may add fields within a schema version).
- Archived flags are never present in a snapshot.
- `fallthrough` is a Serve object: `{"value": bool}`, `{"split": Split}`, or `{}` (serve `true`).
  Setting both `value` and `split` is invalid.
- `Split` is `{"variations": [{"value": bool, "weight": int}], "bucket_by": "user_id" |
  "attr:<name>", "salt": string}`, with weights in units of 0.001% summing to 100000.

## 2. Algorithm

Given a snapshot, a flag key, and a context `{user_id, attributes}`:

1. If the snapshot is not loaded: `value=false`, `reason=ERROR`, `error_code=GENERAL`.
2. If the key appears more than once in the snapshot: `value=false`, `reason=ERROR`,
   `error_code=PARSE_ERROR`.
3. If the key is absent (keys are case-sensitive): `value=false`, `reason=ERROR`,
   `error_code=FLAG_NOT_FOUND`, `flag_version=0`.
4. If `enabled` is false: `value=false`, `reason=DISABLED`, whatever the configuration.
5. If the configuration is invalid, or uses a feature this SDK does not implement:
   `value=false`, `reason=ERROR`, `error_code=PARSE_ERROR`. Other flags MUST be unaffected.
6. Rules, in order (version 1 SDKs implement none and apply step 5 to any rule).
7. Otherwise serve `fallthrough`: a fixed value gives `reason=STATIC`; `{}` gives `value=true`,
   `reason=STATIC`.

Every result carries the flag's `version` as `flag_version` (0 when the flag is absent or its
key is duplicated). An SDK MUST NOT throw or panic on evaluation, and SHOULD NOT allocate or
block.

## 3. Bucketing

For percentage splits (specified now, evaluated from a later version):

```
bucket = uint32_big_endian(SHA-256(utf8(flag_key + "." + salt + "." + value))[0:4]) mod 100000
```

`value` is the context's `user_id` when `bucket_by` is `user_id`, or the string form of attribute
`<name>` for `attr:<name>`. A split whose bucketing value is empty or missing serves its first
variation with `reason=DEFAULT`; it is never random. `bucket_cases` in the vectors pin this
function.

## 4. Vectors file

`testdata/vectors.json` has `vectors_version`, `evaluation_cases` (each a snapshot document, a
`flag`, a `context`, and `expected` `{value, reason, error_code, flag_version}`, where an empty
`error_code` means none), and `bucket_cases` (`flag_key`, `salt`, `value`, `expected`).
