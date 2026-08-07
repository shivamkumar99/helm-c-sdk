# ADR-0004: Data returns via out-params; options via JSON strings

- Status: **Accepted** (2026-08-07, decided by project owner)

## Context
Two ABI shapes were open (PLAN §7), with an earlier design round leaning the other way
(uplink-c-style `Helm*Result` structs; per-option setter functions).

## Decision
1. **Results ride typed out-params.** Data-returning functions keep the ADR-0003 shape:
   `int32_t helm_x(..., <type>* result_out, char** error_out)` — `char**` for
   strings/JSON, `helm_handle_t*` (i.e. `uint64_t*`) for handles. No struct-by-value
   returns anywhere in the ABI.
2. **Optional parameters ride a single `const char* opts_json`.** Keys are documented per
   function in docs/API.md, mirror the SDK action fields, and are **additive forever**.
   `NULL`/empty means all defaults. Parsing is strict (`DisallowUnknownFields`): an unknown
   key is `HELM_ERR_INVALID_ARG`, so typos fail loudly instead of being ignored.

## Consequences
- Uniform with Phase 0; trivial from ctypes/N-API/Swift — no struct marshalling at all.
- Tiny, stable symbol count; new options never add symbols.
- Option typos surface at runtime, not compile time — mitigated by strict parsing.
- Supersedes the earlier result-struct/setter leaning; recorded here so it is not
  relitigated by accident.
