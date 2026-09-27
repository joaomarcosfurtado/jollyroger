# jollyroger protocol

Normative documents for anyone implementing a jollyroger SDK or client. The Go implementation in
this repository is tested against every file here; an SDK in another language must be too.

| Document | Contract |
|---|---|
| [evaluation-spec.md](evaluation-spec.md) | the snapshot format, the evaluation algorithm, and bucketing |
| [testdata/vectors.json](testdata/vectors.json) | cases every SDK must pass, byte for byte |
| [storage-contract.md](storage-contract.md) | the tables, columns and query an SDK may read from the host database |

Planned (later milestones): `openapi.yaml` (HTTP API v1), `metrics.md` (shared metric names).

Versioning: each document carries a version. Within a version, changes are additive only.
