# ADR-0003: Stable error codes + optional `char** error_out`

- Status: **Accepted** (2026-08-07)

## Context
Every FFI host needs (a) a machine-checkable failure signal and (b) human-readable detail,
without thread-affinity traps. A thread-local "last error" (à la `errno`/OpenSSL) breaks
under Node's libuv thread pool, where the call and the error read may run on different
threads.

## Decision
- Fallible functions return `int32_t`: `0` = `HELM_OK`, failures are negative values of the
  frozen `helm_error_code` enum (mirrored by `internal/cerrors.Code`; append-only).
- Detail rides an optional final `char** error_out` (malloc'd, caller frees with
  `helm_free_string`; `NULL` to decline). Fully stateless → fully thread-safe.
- Go-side, codes attach to errors via `cerrors.CodedError` (`errors.As`-recoverable
  through wrapping); unmapped errors surface as `HELM_ERR_UNKNOWN`.
- Every `//export` shim carries a `recover()` guard mapping panics to `HELM_ERR_PANIC` —
  a Go panic crossing into C is undefined behavior.

## Consequences
- No hidden state; safe from any thread; trivial to wrap in every host language.
- Two out-params on data-returning functions (result + error) — the result-shape question
  (out-params vs uplink-c-style result structs) is ADR-0004.
