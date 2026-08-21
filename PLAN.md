# PLAN.md — helm-c implementation plan

> The blueprint this project was built from, phase by phase; items are ticked as they
> land. Kept for history and for planning the remaining backlog. The layout has since
> evolved: the Go wrapper layer is public (`pkg/wrapper`, `pkg/cerrors`), and all test
> fixtures are generated at runtime (`internal/testfixtures`) — nothing is committed.

## 1. Goal

A C shared library over the Helm v4 Go SDK (`../helm`, module `helm.sh/helm/v4`) consumable
from Node (N-API), Swift, and Python on Windows/Linux/macOS, with explicit, binding-friendly
memory management.

## 2. Repository layout (target)

```
helm-c/
  PLAN.md                 # this file
  go.mod                  # module github.com/shivamkumar99/helm-c-sdk; helm.sh/helm/v4 PINNED to an exact
                          #   release tag (v4.2.3 as of 2026-08-07) — NO replace, NO branch
                          #   (../helm clone is read-only reference; verify APIs against the pin)
  main.go                 # package main + empty main() (required by c-shared)
  capi/                   # THE ONLY cgo package: //export shims + conversions
    capi.go               #   version query, free functions, leak probe
    convert.go            #   C<->Go conversion helpers (string/JSON/handle/error out-params)
    chart.go strvals.go … #   one file per SDK area, thin shims only
  internal/
    wrapper/              # pure-Go use cases calling helm.sh/helm/v4 (all logic, all tests)
    handles/              # thread-safe handle registry
    cerrors/              # Go error -> stable C error-code mapping
  include/helm_c.h        # hand-maintained public header (stable ABI, doc comments)
  docs/                   # API.md, MEMORY.md, BUILD.md, DESIGN.md
  test/c-harness/         # C program driving the built library like a real binding
  Makefile                # build/test targets per OS
  .github/workflows/ci.yml
```

Key cgo constraint driving this shape: **cgo types do not cross Go package boundaries**, so
every `import "C"` lives in `capi/`; `internal/*` stays pure Go and unit-testable.

## 3. ABI design

### 3.1 Conventions
- All symbols `helm_*`, snake_case. Header guards + `extern "C"` for C++ consumers.
- Strings: UTF-8 `char*`. Complex data: **JSON** in UTF-8 strings (KISS; every FFI host
  parses JSON natively). No exposed struct layouts.
- Stateful Go objects: `typedef uint64_t helm_handle_t;` — opaque ids into a Go registry
  (cgo forbids passing Go pointers to C).

### 3.2 Error model (stateless, thread-safe)
- Fallible functions return `int32_t` status: `0` = OK, negative = `helm_error_code` from a
  frozen enum (e.g. `HELM_ERR_INVALID_ARG`, `HELM_ERR_NOT_FOUND`, `HELM_ERR_CHART_LOAD`,
  `HELM_ERR_RENDER`, `HELM_ERR_KUBE`, `HELM_ERR_STORAGE`, `HELM_ERR_REGISTRY`,
  `HELM_ERR_CANCELLED`, `HELM_ERR_PANIC`, …).
- Human-readable detail via the last out-param `char** error_out` (malloc'd, caller frees
  with `helm_free_string`; NULL error_out = caller doesn't want detail). No thread-local
  "last error" (breaks under Node's thread pool).
- `internal/cerrors` maps SDK sentinel errors (`driver.ErrReleaseNotFound`, …) to codes with
  `errors.Is/As`.

### 3.3 Memory / lifecycle surface
```c
void  helm_free_string(char* s);                 // frees any char* we returned; NULL-safe
int32 helm_handle_free(helm_handle_t h);         // generic free; idempotent, 0-safe
int64 helm_open_handles_count(void);             // leak probe for binding test suites
const char* helm_c_version(void);                // static string, NOT freed by caller
```
Per-type frees (`helm_chart_free`, `helm_config_free`, …) delegate to `helm_handle_free` but
type-check the handle (freeing a chart handle via `helm_config_free` = `HELM_ERR_INVALID_ARG`).
Registry entries carry a type tag for this.

### 3.4 Panic safety & threading
- Every `//export` shim: `defer recover()` → `HELM_ERR_PANIC` + message. Panics must never
  cross into C (UB).
- Registry: `sync.RWMutex`, ids monotonically increasing, never reused (stale-handle use
  returns `HELM_ERR_INVALID_HANDLE`, never a wrong object).
- **Concurrency contract mirrors the SDK exactly**: charts are immutable/safe to share;
  `kube.Interface` is concurrency-safe; one `Configuration` supports parallel actions on
  **different** releases, but concurrent writes to the **same** release can corrupt history
  (`pkg/storage/storage.go`) — callers serialize per release (documented per function).
  Action objects are constructed per call, so no cross-call action state exists to guard.
  Verified with `-race` on every Go test run plus a multi-threaded pthread stress section
  in the C harness. (An earlier sketch of single-owner action handles with a CAS guard was
  dropped when per-call action construction made it unnecessary; C-side TSAN was evaluated
  and rejected — the Go runtime inside the library is not TSAN-instrumented, so it would
  produce false positives rather than findings.)
- Cancellation: `helm_context_new()/helm_context_cancel(h)/helm_context_free(h)` backed by
  `context.Context`; long-running ops (install/upgrade/pull) take an optional context handle
  and use the SDK's `RunWithContext`.

## 4. Implementation phases (mapped to the SDK dependency levels)

### Phase 0 — foundation (no SDK calls) — **DONE 2026-08-07 (verified on macOS)**
- [x] `go.mod` pinned to `helm.sh/helm/v4 v4.2.3` (no `replace`), `capi/` as the package
      main, Makefile, hand-written `include/helm_c.h`
- [x] Logging seam: injected `log/slog` handler, discard by default, wired
      into the SDK via `action.ConfigurationSetLogger` (landed with the Phase 5
      `helm_set_log_handler` bridge; SDK client/progress writers route through
      it too)
- [x] `internal/handles` registry + tests (concurrent `-race`, double-free, stale-handle,
      wrong-type, ids never reused)
- [x] `internal/cerrors` taxonomy (`CodedError`, `FromError`) + tests
- [x] `capi` conversion helpers + panic-recover guard + `helm_free_string` /
      `helm_handle_free` / `helm_open_handles_count` + `helm_c_version` / `helm_sdk_version`
- [x] Walking skeleton through the whole stack: `helm_release_name_validate`
      (capi → wrapper → pinned SDK), tested at every layer incl. the C harness
- [x] `test/c-harness` e2e (versions, NULL-safety, invalid/double free, validation, soak
      loop, leak gate) — passing against the built `libhelm_c.dylib`
- [x] CI: 3-OS matrix (build, -race tests, harness) + golangci-lint + gosec + govulncheck
      + ASan/LSan leak job + CodeQL (go, c-cpp) — *runs once helm-c is a repo root*
- [x] Docs: API.md (all 6 symbols), MEMORY.md, BUILD.md
- [x] Design decisions recorded (consolidated in docs/DESIGN.md)
- Note: cgo is not allowed in `_test.go` files — boundary tests go through the pure-Go
  `capi/testbridge.go` seam (kept unexported, test-only by convention).

### Phase 1 — cluster-free chart operations (SDK levels 0–1) — **DONE 2026-08-07** (fuzz pending)
- [x] `helm_strvals_parse(s, &out_json, &err)` — `strvals.Parse`
- [x] `helm_release_name_validate(name, &err)` — `chartutil.ValidateReleaseName` (Phase 0)
- [x] `helm_chart_load(path, &handle, &err)` — `chart/v2/loader.Load` (dir or archive)
- [x] `helm_chart_metadata(h, &out_json, &err)` / `helm_chart_values(h, &out_json, &err)`
- [x] `helm_chart_save(h, dir, &out_path, &err)` — `chartutil.Save` (round-trip tested)
- [x] `helm_chart_create(name, dir, &out_path, &err)` — `chartutil.Create`
- [x] `helm_chart_free(h)` — type-checked, idempotent
- [x] `helm_lint_run(path, values_json, &out_json, &err)` — `action.NewLint`; findings are
      data in the JSON report, not a call failure
- [x] `helm_package_run(path, opts_json, &out_path, &err)` — `action.NewPackage`;
      strict opts keys: destination/version/app_version
- [x] Fixture chart `testdata/testchart`; harness covers lifecycle + 100× soak loop
- [x] Fuzz targets: `FuzzParseSetString`, `FuzzLoadArchive`, `FuzzValuesRoundTrip`
      (smoke-run clean; 30s each per PR in the CI fuzz job)

### Phase 2 — values & offline rendering (levels 2–3) — **DONE 2026-08-07**
- [x] `helm_chart_merge_values(h, values_json, &out_json, &err)` — `CoalesceValues`
      (defaults + overrides, the install-time composition; single overrides JSON instead
      of the sketched array — KISS)
- [x] `helm_render(h, values_json, opts_json, &out_json, &err)` — `ToRenderValues` +
      `engine.Render` with `DefaultCapabilities`; returns `{template path: manifest}`
      JSON; opts keys name/namespace/revision/is_install/is_upgrade (strict)
- [x] `helm_schema_validate(h, values_json, &err)` — `ValidateAgainstSchema` on the
      coalesced values; fixture `testdata/schemachart` proves pass + violation paths

### Phase 3 — distribution / network (level 4; no cluster) — **core DONE 2026-08-07**
- [x] `helm_registry_client_new(opts_json, &handle, &err)` / `helm_registry_client_free`
      — `registry.NewClient`, silent by default (`ClientOptWriter(io.Discard)`)
- [x] `helm_registry_login/logout(client_h, host, user, pass, opts_json, &err)` —
      integration-tested against the SDK's in-process basic-auth OCI registry
      (`repotest.NewOCIServer`), incl. wrong-password and unauthenticated-push paths
- [x] `helm_pull(client_h, chart_ref, opts_json, &out_json, &err)` — `action.NewPull`;
      HTTP-repo pulls (repo_url + name) and oci:// refs; client_h 0 = default client
- [x] `helm_push(client_h, chart_path, remote, opts_json, &out_json, &err)` —
      `action.NewPushWithOpts`; full OCI login→push→pull round trip tested
- [x] `helm_repo_index_download(repo_url, opts_json, &out_json, &err)` — `repo/v1`
      with a private temp cache dir (never the user's helm cache)
- [x] `helm_chart_verify(path, prov_file, keyring, &out_json, &err)` — provenance verify
      (fixtures copied from the SDK's `pkg/downloader/testdata` into `testdata/signing/`)
- [x] `helm_dependency_update/build(chart_dir, opts_json, &err)` — `downloader.Manager`;
      auto-registers the chart's HTTP(S) dependency repos in a private temp
      repositories.yaml (Manager rejects unregistered URLs: "no repository definition")
- [x] Credentials never echoed in errors/logs; password used for the login call only.

### Phase 4 — cluster operations (levels 5–6) — **core DONE 2026-08-07**
- [x] `helm_config_new(opts_json, &handle, &err)` — `EnvSettings.RESTClientGetter` +
      `action.NewConfiguration(ConfigurationSetLogger(slog.DiscardHandler))` + `cfg.Init`;
      drivers secret/configmap/memory/sql; kubeconfig loading is LAZY (documented) —
      failures surface at the first action
- [x] `helm_config_free(h)`; `helm_context_new/cancel/free` — cancellation handles;
      pre-cancelled contexts are rejected before entering the SDK (v4.2.3 races on that
      path — SetStatus write race between failRelease and the detached install goroutine;
      upstream report drafted in docs/upstream/)
- [x] `helm_install` / `helm_upgrade` — `RunWithContext`, full opts (wait strategy,
      timeout, dry-run strategy, labels, …); release summary JSON with manifest
- [x] `helm_uninstall`, `helm_rollback`
- [x] `helm_list`, `helm_status`, `helm_history`, `helm_get_values` — read-only; every
      action checks reachability first (unreachable cluster = defined error, tested)
- [x] `helm_get_metadata` (opts: revision)
- [ ] `helm_release_test` (deferred — low-value until bindings ask)
- [x] `kind`-backed CI job (`go test -tags kind`, `helm/kind-action`) driving
      install/upgrade/rollback/uninstall against a real cluster; local tests use the
      memory driver + `kube/fake` clients (kind not installed on this machine — the
      tagged test is vet-verified and runs in CI)
- [x] Options ride `opts_json` with documented keys mirroring SDK action fields.
- Note: `chart.Accessor.MetadataAsMap()` keys by Go field name (`Version`,
  `AppVersion`), not YAML names — the SDK's `structToMap` uses `reflect` field names.

### Maintenance note — stdlib CVEs reach shipped binaries
The library ships compiled standard-library code, so a Go stdlib CVE is real
for everyone who installs a released artifact. When govulncheck turns red
(exit 3), bump the `toolchain` line in go.mod to the release named in the
report, then **re-run the Release workflow** so the published binaries are
rebuilt — patching the source alone does not fix what users already have.
Last bump: go1.26.6 on 2026-08-15 (asn1 recursion, net/http IDNA, two
crypto/tls issues).

### Distribution backlog (raised while planning the Python binding)
- [ ] Widen the release matrix beyond linux-amd64/darwin-arm64/windows-amd64:
      linux-arm64 (arm runner or `zig cc` cross-compile), darwin-amd64
      (macos-13 runner), musllinux (Alpine). Build Linux artifacts in a
      manylinux container (or zig against an old glibc) so one .so runs on
      all mainstream glibc distros — a plain ubuntu-latest build binds to a
      new glibc and fails on older systems.
- [x] Document the from-source path (README "Unsupported platforms" +
      docs/BUILD.md): `make build` works anywhere Go runs, and consumers point
      at the artifact — e.g. the Python binding's `HELM_C_LIB` override.

### Phase 5 — polish for binding consumers (C ABI only — the project rules) — **DONE 2026-08-07**
- [x] Log callback bridge: `helm_set_log_handler(cb, user_data, min_level)` — a
      `slog.Handler` forwarding to a C function pointer via `capi/logbridge.c` (cgo forbids
      C definitions in an exporting file's preamble); level filtering before the C hop;
      silent by default; applies to configs created after the call. Boundary-tested with a
      C capture callback.
- [x] Prebuilt binaries per OS/arch: `.github/workflows/release.yml` (tag push → 3-OS
      build, test, tar with header + docs + pkg-config, attach to GitHub release);
      `make pkgconfig` generates `helm_c.pc`
- [x] Consumer documentation: ownership matrix (docs/MEMORY.md), per-function
      threading/blocking notes (docs/API.md incl. the log-callback contract) —
      **no Node/Python/Swift binding code in this repo** (bindings are downstream projects)
- [ ] `helm_release_test` (still deferred — low value until a binding asks)

Each phase = implement → tests (unit + boundary + harness) → docs (API.md + header) → CI
green on 3 OSes → then next phase. A phase is never left half-documented.

## 5. Build & CI

- **Build:** `go build -buildmode=c-shared -o build/<lib>` per OS
  (`libhelm_c.so` / `libhelm_c.dylib` / `libhelm_c.dll`); ship our hand-written `include/helm_c.h`
  (the cgo-generated header is an internal artifact — ours is the stable, documented one).
- **CI (GitHub Actions), every PR:**
  - matrix `{ubuntu-latest, macos-latest, windows-latest}`: setup Go, (Windows: mingw-w64 via
    MSYS2), `go vet`, golangci-lint, `go test -race ./...`, build the shared lib, compile and
    run `test/c-harness` against it, upload artifacts
  - SAST job: gosec, staticcheck, govulncheck, CodeQL (`go` **and** `c-cpp` for the header/
    harness), osv-scanner; secret scanning via gitleaks + trufflehog; SBOM via syft
  - Linux sanitizer job: harness under ASan/LSan (leak + double-free detection) + TSAN stress
  - scheduled job: `go test -fuzz` corpus runs
  - (No classic web DAST — this is a library; sanitizers + fuzzing + the C harness are the
    dynamic-testing equivalent, (this is the dynamic-testing strategy for a native library).)
- Actions pinned; lockfile (`go.sum`) committed; toolchain versions fixed in CI and BUILD.md.

## 6. Testing strategy

Four mandatory kinds per exported function: unit, integration, e2e, leak.

| Layer | Kind | What | How |
|---|---|---|---|
| `internal/wrapper` | unit | SDK-facing logic | pure-Go table-driven testify tests |
| `internal/handles` | unit | registry | concurrency (`-race`), double-free, stale handle |
| `capi` | unit | conversions, panic guard, ownership | cgo test files in-package |
| chart/repo/cluster ops | integration | real fixtures & services | `testdata/` charts (incl. hostile archives), `repotest` OCI/repo servers, `HELM_DRIVER=memory` + `kind` in CI |
| C harness | e2e | real FFI usage of the built lib | C program: load→call→free→double-free→pthreads; all 3 OSes; must touch every exported symbol |
| Fuzz | dynamic | untrusted inputs | strvals, chart archives, values JSON |
| Leak gate | leak | no handle/string leaks | `helm_open_handles_count()==0` after every run; ASan/LSan + TSAN job; soak loop (create→use→free × N, stable memory) |

## 7. Open decisions (to settle in Phase 0 ADRs)

An earlier design round (before this folder was recreated) had already settled some of these
differently. Where this plan diverges, the ADR must pick one deliberately:

1. ~~**Result shape**~~ — **RESOLVED (2026-08-07):** status + typed out-params;
   no struct-by-value returns.
2. ~~**Action options**~~ — **RESOLVED (2026-08-07):** single `opts_json` with
   strict (`DisallowUnknownFields`) parsing; keys additive forever.
3. **RESOLVED (owner, 2026-08-07):** helm-c becomes its own GitHub repo on the owner's
   account — module path rename pending the account/repo name; then `git init` + push
   (activates all workflows).
4. ~~Kubeconfig input~~ — **RESOLVED (owner: support everything the SDK supports).**
   Done: full `EnvSettings` surface (path, inline content via private 0600 temp file
   cleaned on free, context, bearer token, apiserver/CA/TLS-server-name/insecure,
   impersonation user+groups, burst/qps) + client-go in-cluster fallback.
5. ~~Install chart source~~ — **RESOLVED (owner: support all).** Done: chart handle OR
   chart_ref (local path / repo name + chart_repo_url / oci://) via `LocateChart`,
   for both install and upgrade.
6. ~~Callback ABI~~ — **RESOLVED:** function-pointer callback (shipped as
   `helm_set_log_handler`); explanation of the trade-off vs. polling given to owner —
   a polling queue can be added later as a new function without breaking the ABI.
7. ~~Windows toolchain~~ — **RESOLVED (owner: both).** CI builds with MSYS2 mingw-w64
   gcc (main matrix) AND llvm-mingw clang (dedicated job, pinned release).
8. ~~Testing strategy~~ — **RESOLVED:** memory driver + `kube/fake` for every PR
   (fast, no cluster); `kind` job for real e2e. Bindings are unaffected — tests never
   ship in the library.
