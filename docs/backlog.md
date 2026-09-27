# Backlog

Only work that is still to do. Remove an item when it ships; add one (what + why deferred) when
work is deferred or discovered. See [documentation](skills/documentation.md).

- Write the M3 (client: cache, poller, failure semantics) implementation plan.
- Milestones M3 to M8 as listed in the README roadmap.
- Decide the bucketing input encoding before the rollout milestone: `flagKey + "." + salt + "." + value` is not injective when keys or values contain dots (for example `("a.b","","c")` and `("a","b",".c")` collide). Deferred because nothing buckets yet; changing it after an SDK ships breaks assignments.
- Validate snapshot flag keys on read (empty or oversized keys are accepted and counted by `Len()`). Deferred: harmless for evaluation, and the store (M2) validates keys on write.
