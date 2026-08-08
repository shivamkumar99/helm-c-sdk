# helm-c — memory ownership model

The contract every language binding builds on. See the project rules for the governing rules.

## The two shapes that cross the boundary

| Shape | C type | Allocated by | Freed by caller with | Notes |
|---|---|---|---|---|
| Data (results, errors, JSON) | `char*` (UTF-8) | library (`malloc` via cgo) | `helm_free_string` | exactly once; NULL-safe |
| Stateful Go object | `helm_handle_t` (`uint64_t`) | library (registry entry) | `helm_handle_free` / typed `helm_*_free` | idempotent; 0 is never valid |

Nothing else crosses: no exposed struct layouts, no Go pointers (forbidden by cgo — the GC
may move or collect them), no callbacks holding Go memory.

## Rules

1. **Strings we return are yours to free — exactly once, with our function.**
   `helm_free_string` is NULL-safe, but freeing the same non-NULL pointer twice is
   undefined behavior (it is raw `malloc` memory). Bindings must null their reference
   after freeing. Never use the host `free()` — on Windows the DLL and host may use
   different allocators.
2. **Strings you pass in are borrowed.** The library copies what it needs during the call
   and never keeps your pointer; you may free your buffer as soon as the call returns.
3. **Handles are idempotent to free.** Double-free or freeing 0 returns
   `HELM_ERR_INVALID_HANDLE` and does nothing — safe under nondeterministic GC finalizers.
   Handle ids are never reused, so a stale handle can never alias a newer object.
4. **No auto-cleanup at the C layer.** GC integration belongs in each binding:
   - Node: `napi_add_finalizer` → `helm_*_free`
   - Python: capsule destructor / `weakref.finalize` → `helm_*_free`
   - Swift: `deinit` → `helm_*_free`
5. **The leak gate.** `helm_open_handles_count()` reports live handles; binding test suites
   must assert it returns 0 at shutdown. CI additionally runs the C harness under
   ASan/LSan (`make leak-check`).

## Error out-params

Every fallible function takes a final optional `char** error_out`:
- pass `NULL` if you don't want detail;
- on success it is set to `NULL`;
- on failure it receives a caller-frees message (rule 1 applies).
The `int32_t` return is the stable machine-readable code; the message is human-readable
detail and never contains secrets.
