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
being silently ignored. The alternative (per-type result structs returned by value plus
per-option setter functions) was considered and rejected: more symbols, struct-by-value
FFI requirements, no offsetting benefit.

## 5. Layering

`capi/` is the only package that imports `"C"` (cgo types do not cross Go package
boundaries); its shims only convert and delegate. All logic lives in `pkg/wrapper` —
importable pure Go, usable directly by Go programs — with `pkg/cerrors` carrying the
public error-code taxonomy and `internal/handles` the registry. Test fixtures (charts,
kubeconfig, PGP signing material) are generated at runtime by `internal/testfixtures`;
the repository commits no test data.

## 6. A config handle's SDK configuration is never mutated by an action

The SDK's `action.Configuration` is designed for a CLI process that builds one per
command and exits. Two SDK behaviours are unsafe for a long-lived handle shared across
calls: a client-side dry run replaces the configuration's kube client and release
storage with fakes (`Install.RunWithContext`), and `helm list -A` re-initializes the
configuration for the empty namespace. Applied to a shared handle, the first silently
routes every later action to fakes; the second changes the namespace under other callers.

helm-c therefore keeps the options a config was built from and derives **sibling
configurations** on demand: a fresh `Init` for another namespace or for all namespaces,
and a **detached copy** (same clients and storage, its own struct) for any SDK call known
to mutate its configuration. The shared configuration is read, never written, by an
action. Cost: one extra `Init` per cross-namespace call, which does not contact the
cluster (client loading is lazy).

## 7. Remote chart references are pulled through helm-c's own path

The SDK's `ChartPathOptions.LocateChart` resolves `repo/chart` names through the host
user's `repositories.yaml`, downloads into the user's helm cache, and writes progress to
`os.Stdout` — three things a library loaded into a host process must not do. helm-c
resolves a `chart_ref` itself: local paths load directly; remote references (repo chart +
`chart_repo_url`, or `oci://`) go through `PullChart` into a private scratch directory
with the same silent logging as every other operation, and the scratch directory is
removed once the chart is in memory. `EnvSettings` are built in exactly one place
(`newSettings`), where the repository config and caches are pointed away from the user's
helm home, so the policy cannot regress one call site at a time. The single deliberate
exception is plugin post-renderers, which by definition live in the user's plugin
directory.

## Use of unsafe

The `capi` package imports `unsafe` in exactly three shapes, all mandated by the
cgo FFI contract and none reachable from library logic:

1. `C.free(unsafe.Pointer(...))` (convert.go `freeCString`, logging.go) — the
   only way to release memory allocated with `C.CString`; `C.free`'s signature
   takes `unsafe.Pointer`. The pointers are our own allocations, freed exactly
   once, never dereferenced afterward.
2. `unsafe.Pointer` as an opaque user-data slot for the C log callback
   (logging.go). The value is supplied by C, stored, and passed back verbatim —
   Go never dereferences it.
3. `C.GoBytes(unsafe.Pointer(data), n)` in `helm_chart_load_archive` (extra.go)
   — copies a caller-owned buffer of the stated, bounds-checked length into Go
   memory; the C pointer is never retained beyond the call.

The Go test seam (`testbridge*.go`) additionally hands a Go buffer's address to
that shim for the duration of one call. The seam is compiled into the library
(cgo is not permitted in `_test.go` files) but consists only of unexported Go
functions — it adds no C symbol and is unreachable from any host.

## Scanner findings accepted by design

The following are reported by static analysis on every run and are won't-fix;
each is explained at its site:

- `helm_install` / `helm_upgrade` take 9 parameters — the frozen C ABI.
- The `unsafe` uses above — mandated by the cgo FFI contract.
- Bounded `memcpy` in `test/c-harness` — a test program copying into a buffer
  whose capacity it has just checked; the guard is documented inline.
- Tests reading files they created moments earlier under a temp directory.

Security scanners flag any `unsafe` import (e.g. gosec G103); those findings are
accepted-by-design here with inline `#nosec G103` annotations at each site. No
pointer arithmetic, no type punning, and no `unsafe.Slice`/`unsafe.String`
constructions from C-controlled lengths occur outside bounds-checked helpers.
