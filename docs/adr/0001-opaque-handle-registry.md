# ADR-0001: Opaque handle registry for stateful Go objects

- Status: **Accepted** (2026-08-07)

## Context
cgo forbids passing Go pointers to C (the GC may move or collect them), yet the wrapper
must hand stateful SDK objects (charts, action configurations, registry clients) to
Node/Swift/Python callers, whose GCs free things late, out of order, and on any thread.

## Decision
A thread-safe registry (`internal/handles`) maps monotonically increasing `uint64` ids to
Go objects. C sees only `helm_handle_t`. Entries are type-tagged; ids start at 1 and are
**never reused**; `Free` is a defined error (not a crash) for unknown/already-freed ids.

## Consequences
- Double-free from GC finalizers is safe by construction; stale handles can only miss,
  never alias a newer object.
- `helm_open_handles_count` gives bindings a leak gate.
- Cost: one mutex-guarded map lookup per call — negligible next to any SDK operation.
