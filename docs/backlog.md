# Backlog

Only work that is still to do. Remove an item when it ships; add one (what + why deferred) when
work is deferred or discovered. See [documentation](skills/documentation.md).

- Write the M3 (client: cache, poller, failure semantics) implementation plan.
- Milestones M3 to M8 as listed in the README roadmap.
- Decide the bucketing input encoding before the rollout milestone: `flagKey + "." + salt + "." + value` is not injective when keys or values contain dots (for example `("a.b","","c")` and `("a","b",".c")` collide). Deferred because nothing buckets yet; changing it after an SDK ships breaks assignments.
- Validate snapshot flag keys on read (empty or oversized keys are accepted and counted by `Len()`). Deferred: harmless for evaluation, and the store (M2) validates keys on write.
- After a failed ROLLBACK on a manually begun transaction (`migrate.Run`, SQLite `withConn`), discard the connection (`conn.Raw` returning `driver.ErrBadConn`) so the host pool never gets a connection inside a transaction. Deferred: pgx already discards it, and a failed ROLLBACK on a live SQLite connection is rare.
- Reject NUL (`\u0000`) in audit values and config text at validation (M4): PostgreSQL JSONB refuses it while SQLite and memory accept it.
- Document in the SQLite store and `migrations.md` that the raised `busy_timeout` stays on the host's pooled connections, and that SQLite foreign keys are only enforced if the host enables them.
- Read `GetFlag` (outside a transaction) in one read transaction on PostgreSQL too, so flag and states always belong together.
- Record migration `applied_at` with the same precision as other timestamps; reject `information_schema` and `pg_*` names in `postgres.New`.
