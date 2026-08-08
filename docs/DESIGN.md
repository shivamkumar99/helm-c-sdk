# helm-c-sdk — design decisions

The decisions that shape the ABI and the codebase, and why. Changing any of these is a
breaking event — do it deliberately, and update this file in the same change.

## 1. Opaque handle registry for stateful Go objects

cgo forbids passing Go pointers to C (the GC may move or collect them), yet the wrapper
must hand stateful SDK objects — charts, action configurations, registry clients — to
callers whose garbage collectors free things late, out of order, and on any thread.

A thread-safe registry (`internal/handles`) maps monotonically increasing `uint64` ids to
Go objects; C sees only `helm_handle_t`. Entries are type-tagged, ids start at 1 and are
**never reused**, and freeing an unknown or already-freed id is a defined error, never a
crash. Consequences: double-free from GC finalizers is safe by construction; a stale
handle can only miss, never alias a newer object; `helm_open_handles_count` gives binding
test suites a leak gate. Cost: one mutex-guarded map lookup per call — negligible next to
any SDK operation.

## 2. The Helm SDK is pinned to an exact release

`go.mod` requires `helm.sh/helm/v4` at an exact published tag (`v4.2.3`) with no
`replace` and no branch tracking, so "which SDK version does this build contain?" always
has one answer — for CVE response, binding compatibility, and reproducible builds.
`helm_sdk_version()` exposes the pin at runtime from Go build info. APIs are verified
against the pinned release's source; a symbol that exists on Helm `main` but not in the
pin does not exist for this project. Pin bumps are standalone changes run through the
full three-OS CI gate.

## 3. Error model: stable codes + optional `char** error_out`

Every FFI host needs a machine-checkable failure signal and human-readable detail,
without thread-affinity traps — a thread-local "last error" (à la `errno`) breaks under
Node's libuv thread pool, where the call and the error read may run on different threads.

Fallible functions return `int32_t`: `0` = `HELM_OK`, failures are negative values of the
frozen `helm_error_code` enum (append-only; never renumber). Detail rides an optional
final `char** error_out` (malloc'd, caller frees with `helm_free_string`; `NULL` to
decline) — fully stateless, therefore fully thread-safe. Go-side, codes attach to errors
via `cerrors.CodedError` and survive wrapping; unmapped errors surface as
`HELM_ERR_UNKNOWN`. Every `//export` shim carries a `recover()` guard mapping panics to
`HELM_ERR_PANIC`, because a Go panic crossing into C is undefined behavior.

## 4. Data via out-params; options via strict JSON

Results ride typed out-params (`char**` for strings/JSON, `helm_handle_t*` for handles) —
no struct-by-value returns anywhere in the ABI, so ctypes/N-API/Swift consumers never
marshal structs. Optional parameters ride a single `const char* opts_json` whose keys are
documented per function, mirror the SDK action fields, and are **additive forever**.
Parsing is strict (unknown key = `HELM_ERR_INVALID_ARG`), so typos fail loudly instead of
being silently ignored. The alternative (uplink-c-style result structs and per-option
setter functions) was considered and rejected: more symbols, struct-by-value FFI
requirements, no offsetting benefit.

## 5. Layering

`capi/` is the only package that imports `"C"` (cgo types do not cross Go package
boundaries); its shims only convert and delegate. All logic lives in `pkg/wrapper` —
importable pure Go, usable directly by Go programs — with `pkg/cerrors` carrying the
public error-code taxonomy and `internal/handles` the registry. Test fixtures (charts,
kubeconfig, PGP signing material) are generated at runtime by `internal/testfixtures`;
the repository commits no test data.
