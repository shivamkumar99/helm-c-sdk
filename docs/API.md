# helm-c — C API reference

One entry per exported symbol (updated in the same change as the code).
General ownership rules live in [MEMORY.md](MEMORY.md); error codes are the
`helm_error_code` enum in [include/helm_c.h](../include/helm_c.h).

Conventions used below:
- **borrowed** — the library reads the value during the call only; caller keeps ownership.
- **caller-frees** — malloc'd by the library; the caller must free exactly once with
  `helm_free_string`.
- All string parameters and results are UTF-8.
- `error_out` params are optional: pass `NULL` to decline detail. On success `*error_out`
  is set to `NULL`; on failure it receives a caller-frees message.

---

## helm_c_version

```c
char* helm_c_version(void);
```
Returns the helm-c library version (e.g. `"0.1.0"`).

| | |
|---|---|
| Returns | version string, **caller-frees** |
| Errors | none (never NULL) |
| Thread-safe | yes |
| Blocking | no |
| Since | 0.1.0 |

```c
char* v = helm_c_version();
printf("%s\n", v);
helm_free_string(v);
```

## helm_sdk_version

```c
char* helm_sdk_version(void);
```
Returns the exact `helm.sh/helm/v4` release this library was compiled against
(e.g. `"v4.2.3"`), read from Go build info.

| | |
|---|---|
| Returns | version string, **caller-frees**; `"unknown"` if build info is unavailable |
| Errors | none (never NULL) |
| Thread-safe | yes |
| Blocking | no |
| Since | 0.1.0 |

## helm_free_string

```c
void helm_free_string(char* s);
```
Frees any `char*` previously **returned by this library**. `NULL`-safe.

| | |
|---|---|
| `s` | pointer previously returned by a helm-c function, or `NULL` |
| Thread-safe | yes |
| Since | 0.1.0 |

Rules: free each pointer **exactly once** (passing the same non-NULL pointer twice is
undefined — bindings must null their reference after freeing); never pass a pointer that
did not come from this library; never use the host's own `free()` (allocators may differ
across DLL boundaries on Windows).

## helm_handle_free

```c
int32_t helm_handle_free(helm_handle_t handle, char** error_out);
```
Releases the Go object behind a handle of **any** type. Idempotent: an unknown, zero, or
already-freed handle returns `HELM_ERR_INVALID_HANDLE` and does nothing (GC finalizers may
double-free safely).

| | |
|---|---|
| `handle` | any handle previously returned by this library |
| `error_out` | optional, **caller-frees** on failure |
| Returns | `HELM_OK` or `HELM_ERR_INVALID_HANDLE` |
| Thread-safe | yes |
| Blocking | no |
| Since | 0.1.0 |

## helm_open_handles_count

```c
int64_t helm_open_handles_count(void);
```
Number of currently live handles. Binding test suites assert `0` at shutdown to prove they
leak nothing (the leak gate).

| | |
|---|---|
| Returns | live handle count, ≥ 0 |
| Thread-safe | yes |
| Since | 0.1.0 |

## helm_release_name_validate

```c
int32_t helm_release_name_validate(const char* name, char** error_out);
```
Validates `name` against Helm's release-name rules (max 53 chars, DNS-label charset), via
the SDK's `chartutil.ValidateReleaseName`.

| | |
|---|---|
| `name` | **borrowed** UTF-8; `NULL` → `HELM_ERR_INVALID_ARG` |
| `error_out` | optional, **caller-frees** on failure |
| Returns | `HELM_OK` or `HELM_ERR_INVALID_ARG` |
| Thread-safe | yes |
| Blocking | no |
| Since | 0.1.0 |

```c
char* err = NULL;
if (helm_release_name_validate("My_Bad_Name", &err) != HELM_OK) {
    fprintf(stderr, "invalid: %s\n", err);
    helm_free_string(err);
}
```

## helm_strvals_parse

```c
int32_t helm_strvals_parse(const char* s, char** out, char** error_out);
```
Parses a Helm `--set` expression (`"a=1,b.c=two,ports={80,443}"`) into a JSON object,
via the SDK's `strvals.Parse`.

| | |
|---|---|
| `s` | **borrowed**; `NULL` → `HELM_ERR_INVALID_ARG` |
| `out` | required; receives JSON, **caller-frees** |
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_VALUES` (malformed expression) |
| Thread-safe | yes · Blocking: no · Since 0.1.0 |

## helm_chart_load

```c
int32_t helm_chart_load(const char* path, helm_handle_t* out, char** error_out);
```
Loads a chart from a directory or `.tgz` archive (SDK `loader.Load`). Decompression
bombs/zip-slip are bounded by the SDK's built-in limits.

| | |
|---|---|
| `path` | **borrowed** filesystem path |
| `out` | required; receives a chart handle, freed with `helm_chart_free` |
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_CHART_LOAD` |
| Thread-safe | yes; the resulting chart is immutable — share freely, free once |
| Blocking | filesystem I/O · Since 0.1.0 |

## helm_chart_metadata / helm_chart_values

```c
int32_t helm_chart_metadata(helm_handle_t chart, char** out, char** error_out);
int32_t helm_chart_values(helm_handle_t chart, char** out, char** error_out);
```
JSON of the chart's `Chart.yaml` metadata (name, version, appVersion, …) / default
values.

| | |
|---|---|
| `chart` | a live chart handle |
| `out` | required; receives JSON, **caller-frees** |
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_INVALID_HANDLE`, `HELM_ERR_WRONG_HANDLE_TYPE` |
| Thread-safe | yes · Blocking: no · Since 0.1.0 |

## helm_chart_save

```c
int32_t helm_chart_save(helm_handle_t chart, const char* dest_dir,
                        char** out_path, char** error_out);
```
Archives the chart into `dest_dir` (SDK `chartutil.Save`); `*out_path` receives the
`.tgz` path.

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_INVALID_HANDLE`, `HELM_ERR_WRONG_HANDLE_TYPE`, `HELM_ERR_IO` |
| Thread-safe | yes · Blocking: filesystem I/O · Since 0.1.0 |

## helm_chart_create

```c
int32_t helm_chart_create(const char* name, const char* dir,
                          char** out_path, char** error_out);
```
Scaffolds a new chart named `name` inside `dir` (SDK `chartutil.Create`); `*out_path`
receives the created chart directory. Name is validated first
(`HELM_ERR_INVALID_ARG`); write failures are `HELM_ERR_IO`.

| | |
|---|---|
| Thread-safe | yes · Blocking: filesystem I/O · Since 0.1.0 |

## helm_chart_free

```c
int32_t helm_chart_free(helm_handle_t chart, char** error_out);
```
Type-checked free of a chart handle. A non-chart handle returns
`HELM_ERR_WRONG_HANDLE_TYPE` **and stays alive**; double-free returns
`HELM_ERR_INVALID_HANDLE` harmlessly.

| | |
|---|---|
| Thread-safe | yes · Since 0.1.0 |

## helm_lint_run

```c
int32_t helm_lint_run(const char* path, const char* values_json,
                      char** out, char** error_out);
```
Lints the chart at `path` (SDK `action.Lint`). `values_json` is optional (`NULL` = no
overrides). Lint findings are **data in the report**, not a call failure:

```json
{"total_charts_linted":1,
 "messages":[{"severity":1,"path":"values.yaml","error":"..."}],
 "errors":["..."]}
```
Severity: 0=unknown, 1=info, 2=warning, 3=error. Report fields are additive-only (ABI).

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_VALUES` (bad values_json) |
| Thread-safe | yes · Blocking: filesystem I/O · Since 0.1.0 |

## helm_package_run

```c
int32_t helm_package_run(const char* path, const char* opts_json,
                         char** out_path, char** error_out);
```
Packages the chart at `path` into a `.tgz` (SDK `action.Package`); `*out_path` receives
the archive path. `opts_json` keys (all optional; unknown keys →
`HELM_ERR_INVALID_ARG`; see docs/DESIGN.md §4):

| key | meaning | default |
|---|---|---|
| `destination` | output directory | `"."` |
| `version` | override chart version | chart's own |
| `app_version` | override appVersion | chart's own |

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_CHART_INVALID` |
| Thread-safe | yes · Blocking: filesystem I/O · Since 0.1.0 |

## helm_chart_merge_values

```c
int32_t helm_chart_merge_values(helm_handle_t chart, const char* values_json,
                                char** out, char** error_out);
```
Coalesces the chart's default values with `values_json` overrides (overrides win, nested
tables merge — SDK `CoalesceValues`, the same composition an install uses). `values_json`
optional (`NULL` = defaults only).

| | |
|---|---|
| `out` | required; receives effective values JSON, **caller-frees** |
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_INVALID_HANDLE`, `HELM_ERR_WRONG_HANDLE_TYPE`, `HELM_ERR_VALUES` |
| Thread-safe | yes · Blocking: no · Since 0.1.0 |

## helm_schema_validate

```c
int32_t helm_schema_validate(helm_handle_t chart, const char* values_json,
                             char** error_out);
```
Validates the effective values (defaults + optional overrides) against the chart's
`values.schema.json` (SDK `ValidateAgainstSchema`). A chart without a schema always
passes. Violation detail (which key, which constraint) rides `error_out`.

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_HANDLE`, `HELM_ERR_WRONG_HANDLE_TYPE`, `HELM_ERR_VALUES` |
| Thread-safe | yes · Blocking: no · Since 0.1.0 |

## helm_render

```c
int32_t helm_render(helm_handle_t chart, const char* values_json,
                    const char* opts_json, char** out, char** error_out);
```
Renders the chart's templates **offline** — no cluster; the `lookup` template function
returns empty results (SDK `ToRenderValues` + `engine.Render`, default capabilities).
`*out` receives `{"<chart>/templates/x.yaml": "<manifest>", ...}` JSON.

`opts_json` keys (all optional; unknown keys → `HELM_ERR_INVALID_ARG`; see docs/DESIGN.md §4):

| key | meaning | default |
|---|---|---|
| `name` | release name | `"release-name"` |
| `namespace` | release namespace | `"default"` |
| `revision` | release revision | `1` |
| `is_install` | render as install | `true` (unless `is_upgrade` set) |
| `is_upgrade` | render as upgrade | `false` |

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_INVALID_HANDLE`, `HELM_ERR_WRONG_HANDLE_TYPE`, `HELM_ERR_VALUES`, `HELM_ERR_RENDER` |
| Thread-safe | yes · Blocking: CPU-bound templating · Since 0.1.0 |

## helm_registry_client_new / helm_registry_client_free

```c
int32_t helm_registry_client_new(const char* opts_json, helm_handle_t* out, char** error_out);
int32_t helm_registry_client_free(helm_handle_t client, char** error_out);
```
Creates/frees an OCI registry client (SDK `registry.NewClient`). Silent by default —
client output is discarded per the logging rule. `opts_json` keys:

| key | meaning | default |
|---|---|---|
| `debug` | verbose client behavior | `false` |
| `plain_http` | registry speaks HTTP, not HTTPS | `false` |
| `credentials_file` | where login credentials are stored | helm's default registry config |

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_REGISTRY`; free: handle errors |
| Thread-safe | yes · Since 0.1.0 |

## helm_registry_login / helm_registry_logout

```c
int32_t helm_registry_login(helm_handle_t client, const char* host,
                            const char* username, const char* password,
                            const char* opts_json, char** error_out);
int32_t helm_registry_logout(helm_handle_t client, const char* host, char** error_out);
```
Basic-auth login/logout against a registry host (e.g. `"localhost:5000"`,
`"registry-1.docker.io"`). Login `opts_json` keys: `insecure` (skip TLS verification),
`plain_http`. Credentials persist in the client's credentials file until logout.
**The password is used for the login call only — never logged, stored by helm-c, or
echoed in errors.**

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, handle errors, `HELM_ERR_REGISTRY` (bad credentials, unreachable host) |
| Thread-safe | yes · Blocking: network I/O · Since 0.1.0 |

## helm_pull

```c
int32_t helm_pull(helm_handle_t client, const char* chart_ref,
                  const char* opts_json, char** out, char** error_out);
```
Downloads a chart (SDK `action.Pull`). Two reference styles:
- HTTP repo: `chart_ref` = chart name + `"repo_url"` in opts
- OCI: `chart_ref` = `"oci://host/path/name"`; `client` may be `0` to use a default
  client (honoring `plain_http`), or a logged-in client handle for private registries.

`opts_json` keys: `dest_dir` (default `"."`), `version`, `repo_url`, `untar`,
`untar_dir`, `plain_http`, `insecure_skip_tls_verify`, `username`, `password`.
`*out` receives `{"output":"..."}` (human-readable summary; the archive lands in
`dest_dir` as `<name>-<version>.tgz`).

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, handle errors, `HELM_ERR_REPO` |
| Thread-safe | yes · Blocking: network I/O (SDK default HTTP timeout 120s) · Since 0.1.0 |

## helm_push

```c
int32_t helm_push(helm_handle_t client, const char* chart_path,
                  const char* remote, const char* opts_json,
                  char** out, char** error_out);
```
Uploads a chart `.tgz` to an OCI remote (SDK `action.Push`). `remote` is the repo root
(`"oci://host/path"`); the chart name and version come from the archive itself. `client`
may be `0` for a default client, but private registries need a logged-in handle.
`opts_json` keys: `plain_http`, `insecure_skip_tls_verify`.

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, handle errors, `HELM_ERR_REGISTRY` (incl. unauthenticated push) |
| Thread-safe | yes · Blocking: network I/O · Since 0.1.0 |

## helm_repo_index_download

```c
int32_t helm_repo_index_download(const char* repo_url, const char* opts_json,
                                 char** out, char** error_out);
```
Fetches and parses a chart repository's `index.yaml` (SDK `repo/v1`). Cache files go to
a private temp dir (removed before returning), never the user's helm cache. `opts_json`
keys: `username`, `password`, `insecure_skip_tls_verify`. `*out` receives the full index
as JSON: `{"apiVersion":"v1","entries":{"<chart>":[{"name","version","urls","digest",…}]}}`.

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_REPO`, `HELM_ERR_IO` |
| Thread-safe | yes · Blocking: network I/O; a large repo returns large JSON · Since 0.1.0 |

## helm_config_new / helm_config_free

```c
int32_t helm_config_new(const char* opts_json, helm_handle_t* out, char** error_out);
int32_t helm_config_free(helm_handle_t config, char** error_out);
```
Builds a cluster-connected action configuration (SDK `Configuration.Init` behind an
`EnvSettings` REST client getter — the wrapper exposes the SDK's **entire** kube
connection surface). `opts_json` keys:

| key | meaning | default |
|---|---|---|
| `kubeconfig_path` | kubeconfig file | client-go chain (see below) |
| `kubeconfig_content` | inline kubeconfig YAML — written to a private `0600` temp file, removed on `helm_config_free` | — |
| `kube_context` | context within the kubeconfig | its current context |
| `kube_token` | bearer token auth | — |
| `kube_apiserver` | API server endpoint override | — |
| `kube_ca_file` | custom CA file | — |
| `kube_tls_server_name` | server-cert name override | contacted hostname |
| `kube_insecure_skip_tls_verify` | skip server cert checks | `false` |
| `kube_as_user` / `kube_as_groups` | impersonation (string / array) | — |
| `burst_limit` / `qps` | client-side throttling | SDK defaults |
| `namespace` | target namespace | `"default"` |
| `storage_driver` | `""`/`"secret"`, `"configmap"`, `"memory"`, `"sql"` | `"secret"` |

`kubeconfig_path` and `kubeconfig_content` are mutually exclusive. With neither, the
client-go default chain applies: `KUBECONFIG` env → `~/.kube/config` → **in-cluster
service account** (when the host process runs inside a pod) — so in-cluster operation
needs no options at all.

**Lazy kubeconfig contract:** creation parses options but does not contact the cluster —
a bad server (or even a garbage kubeconfig) surfaces at the **first action**, not here.

**Concurrency:** one config supports parallel actions on *different* releases;
concurrent writes to the *same* release can corrupt its history (SDK semantics) —
bindings must serialize per release.

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_KUBE`; free: handle errors |
| Thread-safe | yes · Since 0.1.0 |

## helm_context_new / helm_context_cancel / helm_context_free

```c
int32_t helm_context_new(helm_handle_t* out, char** error_out);
int32_t helm_context_cancel(helm_handle_t ctx, char** error_out);
int32_t helm_context_free(helm_handle_t ctx, char** error_out);
```
Cancellation for `helm_install`/`helm_upgrade`: cancel from any thread and a blocked
call returns `HELM_ERR_CANCELLED`. Cancel is repeatable; free also cancels. A context
already cancelled before the call is rejected up front (never enters the SDK — guards a
known v4.2.3 SDK race on pre-cancelled contexts).

| | |
|---|---|
| Thread-safe | yes · Since 0.1.0 |

## helm_install / helm_upgrade

```c
int32_t helm_install(helm_handle_t config, helm_handle_t ctx, helm_handle_t chart,
                     const char* chart_ref, const char* name,
                     const char* values_json, const char* opts_json,
                     char** out, char** error_out);
int32_t helm_upgrade(/* same signature */);
```
Installs / upgrades release `name` (SDK `Install.RunWithContext` /
`Upgrade.RunWithContext`). The chart comes from **either** source (exactly one):
- `chart` ≠ 0 — a loaded chart handle (`chart_ref` NULL);
- `chart` = 0 — `chart_ref` resolved exactly like the helm CLI (`LocateChart`): a local
  directory/`.tgz` path, a repo chart name combined with opts `chart_repo_url` +
  `chart_version`, or an `oci://host/path/name` reference (a default registry client is
  used for anonymous OCI pulls, honoring `plain_http`; give the config a logged-in
  client for private registries).

`ctx` optional (`0` = not cancellable). `*out` receives the release summary JSON
**with manifest**: `{"name","namespace","revision","status","notes","deployed_at",
"chart_name","chart_version","app_version","labels","manifest"}` (additive-only).

Shared `opts_json` keys: `namespace` (default = config's), `timeout_seconds`,
`wait` (`""`/`"watcher"`/`"legacy"`/`"hookOnly"`), `dry_run`
(`""`/`"none"`/`"client"`/`"server"`), `rollback_on_failure`, `description`, `labels`,
`chart_repo_url`, `chart_version`, `plain_http`. Install-only: `create_namespace`.
Upgrade-only: `max_history`, `reset_values`, `reuse_values`, `cleanup_on_fail`.

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, handle errors, `HELM_ERR_VALUES`, `HELM_ERR_CANCELLED`, `HELM_ERR_NOT_FOUND` (upgrade of absent release), `HELM_ERR_RELEASE` |
| Thread-safe | yes across releases; serialize same-release writes · Blocking: cluster I/O · Since 0.1.0 |

## helm_uninstall

```c
int32_t helm_uninstall(helm_handle_t config, const char* name,
                       const char* opts_json, char** out, char** error_out);
```
Removes release `name`. `opts_json` keys: `keep_history`, `timeout_seconds`, `dry_run`
(bool), `ignore_not_found`, `wait`, `description`. `*out` receives
`{"info":"...","release":{summary}}`.

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_NOT_FOUND`, `HELM_ERR_RELEASE`, arg/handle errors |
| Thread-safe | yes (serialize per release) · Blocking: cluster I/O · Since 0.1.0 |

## helm_rollback

```c
int32_t helm_rollback(helm_handle_t config, const char* name,
                      const char* opts_json, char** error_out);
```
Rolls `name` back; creates a **new** revision. `opts_json` keys: `version` (`0` =
previous revision), `timeout_seconds`, `wait`, `dry_run` (string strategy). Status code
only — read the result via `helm_status`.

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_NOT_FOUND`, `HELM_ERR_RELEASE`, arg/handle errors |
| Thread-safe | yes (serialize per release) · Blocking: cluster I/O · Since 0.1.0 |

## helm_list / helm_status / helm_history / helm_get_values

```c
int32_t helm_list(helm_handle_t config, const char* opts_json, char** out, char** error_out);
int32_t helm_status(helm_handle_t config, const char* name, const char* opts_json, char** out, char** error_out);
int32_t helm_history(helm_handle_t config, const char* name, const char* opts_json, char** out, char** error_out);
int32_t helm_get_values(helm_handle_t config, const char* name, const char* opts_json, char** out, char** error_out);
```
Read-only queries. All check cluster reachability first — an unreachable cluster is a
defined error, never a hang. Options:
- list: `all` (every state), `all_namespaces`, `limit`, `offset`, `filter` (regex);
  returns a JSON array of summaries **without** manifests
- status: `revision` (`0` = latest); returns a summary **with** manifest
- history: `max`; returns a JSON array of revision summaries
- get_values: `all` (computed values instead of user-supplied), `revision`

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_NOT_FOUND` (status/history/get_values), `HELM_ERR_RELEASE`, arg/handle errors |
| Thread-safe | yes · Blocking: cluster I/O · Since 0.1.0 |

## helm_get_metadata

```c
int32_t helm_get_metadata(helm_handle_t config, const char* name,
                          const char* opts_json, char** out, char** error_out);
```
Release metadata JSON (SDK `GetMetadata`): `{"name","chart","version","appVersion",
"namespace","revision","status","deployedAt","annotations",...}`. `opts_json` keys:
`revision` (`0` = latest).

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_NOT_FOUND`, `HELM_ERR_RELEASE`, arg/handle errors |
| Thread-safe | yes · Blocking: cluster I/O · Since 0.1.0 |

## helm_dependency_update / helm_dependency_build

```c
int32_t helm_dependency_update(const char* chart_dir, const char* opts_json, char** error_out);
int32_t helm_dependency_build(const char* chart_dir, const char* opts_json, char** error_out);
```
`helm dependency update` / `build` for an unpacked chart directory (SDK
`downloader.Manager`): update resolves declared dependencies into `charts/` and writes
`Chart.lock`; build reconstructs `charts/` from the existing lock. The wrapper
auto-registers the chart's HTTP(S) dependency repositories in a **private temp repo
config** (removed afterwards) — the user's helm config and cache are never touched, and
no `helm repo add` is needed. `opts_json` keys: `skip_refresh`, `keyring`, `verify`,
`plain_http` (for `oci://` dependencies).

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_CHART_LOAD`, `HELM_ERR_REPO`, `HELM_ERR_IO` |
| Thread-safe | yes (distinct chart dirs; don't run two updates on one dir) · Blocking: network I/O · Since 0.1.0 |

## helm_chart_verify

```c
int32_t helm_chart_verify(const char* path, const char* prov_file,
                          const char* keyring, char** out, char** error_out);
```
Verifies a chart archive against its provenance signature (SDK
`downloader.VerifyChart`). `prov_file` may be `NULL` (defaults to `"<path>.prov"`);
`keyring` is a GPG **public** keyring file. `*out` receives
`{"file_name":"x-1.0.0.tgz","file_hash":"sha256:...","signed_by":["Alice <a@x>"]}`.
A missing or bad signature is `HELM_ERR_CHART_INVALID`.

| | |
|---|---|
| Returns | `HELM_OK`, `HELM_ERR_INVALID_ARG`, `HELM_ERR_CHART_INVALID` |
| Thread-safe | yes · Blocking: filesystem I/O · Since 0.1.0 |

## helm_set_log_handler

```c
typedef void (*helm_log_callback)(int32_t level, const char* message, void* user_data);
int32_t helm_set_log_handler(helm_log_callback cb, void* user_data, int32_t min_level);
```
Routes helm-c's internal logging (the helm SDK's `log/slog` records) to a host callback.
The library is **silent by default**; this is the opt-in. Applies to configs created
**after** the call — install the handler before `helm_config_new`. `min_level` is a
`helm_log_level` (`HELM_LOG_DEBUG`=0 … `HELM_LOG_ERROR`=3); records below it never cross
into C. Pass a `NULL` callback to restore silence.

**Callback contract** (binding authors):
- `message` is UTF-8, formatted as `"msg key=value key=value"`, and valid **only for the
  duration of the call** — copy it before returning.
- May be invoked **concurrently from arbitrary threads** (Go goroutines) — must be
  thread-safe and fast.
- Must **not** call back into helm-c (re-entrancy is undefined).
- `user_data` is passed through untouched; its lifetime is the host's responsibility —
  keep it valid until the handler is cleared.

| | |
|---|---|
| Returns | `HELM_OK` |
| Thread-safe | yes (handler swap is atomic w.r.t. in-flight logging) · Since 0.1.0 |

```c
char* out = NULL; char* err = NULL;
if (helm_render(chart, "{\"replicaCount\":3}", "{\"name\":\"myrel\"}", &out, &err) == HELM_OK) {
    /* out is {"testchart/templates/configmap.yaml": "..."} */
    helm_free_string(out);
} else {
    fprintf(stderr, "render failed: %s\n", err);
    helm_free_string(err);
}
```
