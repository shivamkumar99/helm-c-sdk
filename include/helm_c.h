/*
 * helm_c.h — public C API of helm-c, a C wrapper around the Helm v4 Go SDK.
 *
 * ABI rules (append-only. Symbols, signatures, and enum
 * values here are frozen once shipped.
 *
 * Ownership (see docs/MEMORY.md):
 *   - Every char* RETURNED by this library is malloc'd; free it exactly once
 *     with helm_free_string (NULL-safe). Never use your own free().
 *   - Every char* PASSED IN is borrowed for the call only and must be UTF-8.
 *   - Stateful objects are opaque helm_handle_t values; free with the
 *     matching helm_*_free (or helm_handle_free). Handle frees are
 *     idempotent: freeing twice returns HELM_ERR_INVALID_HANDLE, never crashes.
 *   - Optional `char** error_out` receives failure detail (or NULL on
 *     success); pass NULL to decline. Free the detail with helm_free_string.
 *
 * Thread safety: all functions are safe to call from any thread unless a
 * function's documentation says otherwise.
 */
#ifndef HELM_C_H
#define HELM_C_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

#define HELM_C_VERSION_MAJOR 0
#define HELM_C_VERSION_MINOR 1
#define HELM_C_VERSION_PATCH 0

/* Opaque reference to a Go-side object. 0 is never a valid handle. */
typedef uint64_t helm_handle_t;

/*
 * Stable error codes. 0 is success; failures are negative.
 * Mirrors internal/cerrors; never renumber or remove a value.
 */
typedef enum helm_error_code {
    HELM_OK                    = 0,
    HELM_ERR_UNKNOWN           = -1,
    HELM_ERR_INVALID_ARG       = -2,
    HELM_ERR_INVALID_HANDLE    = -3,
    HELM_ERR_WRONG_HANDLE_TYPE = -4,
    HELM_ERR_PANIC             = -5,
    HELM_ERR_CANCELLED         = -6,
    HELM_ERR_NOT_FOUND         = -7,
    HELM_ERR_IO                = -8,

    HELM_ERR_CHART_LOAD        = -20,
    HELM_ERR_CHART_INVALID     = -21,
    HELM_ERR_VALUES            = -22,
    HELM_ERR_RENDER            = -23,

    HELM_ERR_REGISTRY          = -40,
    HELM_ERR_REPO              = -41,

    HELM_ERR_KUBE              = -60,
    HELM_ERR_STORAGE           = -61,
    HELM_ERR_RELEASE           = -62
} helm_error_code;

/* ------------------------------------------------------------------ */
/* Library info                                                        */
/* ------------------------------------------------------------------ */

/* helm-c library version ("0.1.0"). Caller frees with helm_free_string. */
char* helm_c_version(void);

/* Exact helm.sh/helm/v4 version compiled in (e.g. "v4.2.3").
 * Caller frees with helm_free_string. */
char* helm_sdk_version(void);

/* ------------------------------------------------------------------ */
/* Memory / lifecycle                                                  */
/* ------------------------------------------------------------------ */

/* Frees any char* returned by this library. NULL-safe; free each pointer
 * exactly once. */
void helm_free_string(char* s);

/* Frees the object behind any handle type. Idempotent: an unknown or
 * already-freed handle returns HELM_ERR_INVALID_HANDLE and does nothing. */
int32_t helm_handle_free(helm_handle_t handle, char** error_out);

/* Number of live handles. Binding test suites assert 0 at shutdown to prove
 * they leak nothing. */
int64_t helm_open_handles_count(void);

/* ------------------------------------------------------------------ */
/* Chart utilities                                                     */
/* ------------------------------------------------------------------ */

/* Validates `name` against Helm's release-name rules (length and charset).
 * Returns HELM_OK or HELM_ERR_INVALID_ARG. */
int32_t helm_release_name_validate(const char* name, char** error_out);

/* Parses a Helm --set expression ("a=1,b.c=two") into a JSON object.
 * *out receives the JSON (caller frees with helm_free_string). */
int32_t helm_strvals_parse(const char* s, char** out, char** error_out);

/* ------------------------------------------------------------------ */
/* Charts (offline — no cluster required)                              */
/* ------------------------------------------------------------------ */

/* Loads a chart from a directory or .tgz archive. *out receives a chart
 * handle; free with helm_chart_free. Fails with HELM_ERR_CHART_LOAD. */
int32_t helm_chart_load(const char* path, helm_handle_t* out, char** error_out);

/* Writes the chart's Chart.yaml metadata as JSON into *out
 * (caller frees with helm_free_string). */
int32_t helm_chart_metadata(helm_handle_t chart, char** out, char** error_out);

/* Writes the chart's default values as JSON into *out
 * (caller frees with helm_free_string). */
int32_t helm_chart_values(helm_handle_t chart, char** out, char** error_out);

/* Archives the chart into dest_dir. *out_path receives the .tgz path
 * (caller frees with helm_free_string). */
int32_t helm_chart_save(helm_handle_t chart, const char* dest_dir,
                        char** out_path, char** error_out);

/* Scaffolds a new chart named `name` inside `dir`. *out_path receives the
 * created chart directory (caller frees with helm_free_string). */
int32_t helm_chart_create(const char* name, const char* dir,
                          char** out_path, char** error_out);

/* Frees a chart handle. Type-checked: a non-chart handle returns
 * HELM_ERR_WRONG_HANDLE_TYPE and stays alive. Idempotent per handle. */
int32_t helm_chart_free(helm_handle_t chart, char** error_out);

/* Lints the chart at `path`. values_json is optional (may be NULL). *out
 * receives a JSON report: {"total_charts_linted":N,"messages":[{"severity":
 * 0..3,"path":"...","error":"..."}],"errors":["..."]}. Lint findings are data
 * in the report; the call fails only on malformed input. */
int32_t helm_lint_run(const char* path, const char* values_json,
                      char** out, char** error_out);

/* Packages the chart at `path` into a .tgz. opts_json is optional (may be
 * NULL); keys: {"destination":"dir","version":"x.y.z","app_version":"..."} —
 * unknown keys fail with HELM_ERR_INVALID_ARG. *out_path receives the archive
 * path (caller frees with helm_free_string). */
int32_t helm_package_run(const char* path, const char* opts_json,
                         char** out_path, char** error_out);

/* ------------------------------------------------------------------ */
/* Values & offline rendering (no cluster required)                    */
/* ------------------------------------------------------------------ */

/* Coalesces the chart's default values with overrides (values_json optional,
 * may be NULL; overrides win). *out receives the effective values as JSON
 * (caller frees with helm_free_string). */
int32_t helm_chart_merge_values(helm_handle_t chart, const char* values_json,
                                char** out, char** error_out);

/* Validates the effective values (defaults + optional overrides) against the
 * chart's values.schema.json. A chart without a schema always passes.
 * Returns HELM_OK or HELM_ERR_VALUES. */
int32_t helm_schema_validate(helm_handle_t chart, const char* values_json,
                             char** error_out);

/* Renders the chart's templates offline (no cluster; the `lookup` template
 * function returns empty results). values_json is optional. opts_json is
 * optional; keys: {"name":"release-name","namespace":"default","revision":1,
 * "is_install":true,"is_upgrade":false} — unknown keys fail with
 * HELM_ERR_INVALID_ARG. *out receives {"template path":"manifest",...} JSON
 * (caller frees with helm_free_string). */
int32_t helm_render(helm_handle_t chart, const char* values_json,
                    const char* opts_json, char** out, char** error_out);

/* ------------------------------------------------------------------ */
/* OCI registry & chart distribution (network)                         */
/* ------------------------------------------------------------------ */

/* Creates an OCI registry client. opts_json optional; keys:
 * {"debug":false,"plain_http":false,"credentials_file":"path"}. *out receives
 * a handle; free with helm_registry_client_free. */
int32_t helm_registry_client_new(const char* opts_json, helm_handle_t* out,
                                 char** error_out);

/* Frees a registry-client handle (type-checked, idempotent). */
int32_t helm_registry_client_free(helm_handle_t client, char** error_out);

/* Logs the client into a registry host with basic credentials. opts_json
 * optional; keys: {"insecure":false,"plain_http":false}. The password is
 * never logged or echoed back. */
int32_t helm_registry_login(helm_handle_t client, const char* host,
                            const char* username, const char* password,
                            const char* opts_json, char** error_out);

/* Removes the client's stored credentials for host. */
int32_t helm_registry_logout(helm_handle_t client, const char* host,
                             char** error_out);

/* Downloads a chart from an HTTP repo or an oci:// reference. client is
 * optional (0 = a default client is used for OCI refs). opts_json optional;
 * keys: {"dest_dir":".","version":"","repo_url":"","untar":false,
 * "untar_dir":"","plain_http":false,"insecure_skip_tls_verify":false,
 * "username":"","password":""}. *out receives {"output":"..."} JSON
 * (caller frees with helm_free_string). Blocking: network I/O. */
int32_t helm_pull(helm_handle_t client, const char* chart_ref,
                  const char* opts_json, char** out, char** error_out);

/* Uploads a chart .tgz to an oci:// remote (the chart name/version are taken
 * from the archive). client optional (0 = default client). opts_json
 * optional; keys: {"plain_http":false,"insecure_skip_tls_verify":false}.
 * *out receives {"output":"..."} JSON. Blocking: network I/O. */
int32_t helm_push(helm_handle_t client, const char* chart_path,
                  const char* remote, const char* opts_json,
                  char** out, char** error_out);

/* Fetches a chart repository's index.yaml. opts_json optional; keys:
 * {"username":"","password":"","insecure_skip_tls_verify":false}. *out
 * receives the parsed index as JSON ({"apiVersion","entries":{...}}).
 * Blocking: network I/O; large repos return large JSON. */
int32_t helm_repo_index_download(const char* repo_url, const char* opts_json,
                                 char** out, char** error_out);

/* ------------------------------------------------------------------ */
/* Cluster configuration & cancellation                                */
/* ------------------------------------------------------------------ */

/* Builds a cluster-connected configuration. opts_json optional; keys (the
 * SDK's full kube connection surface):
 *   kubeconfig_path, kubeconfig_content (mutually exclusive; content goes to
 *   a private 0600 temp file removed on free), kube_context, kube_token
 *   (bearer), kube_apiserver, kube_ca_file, kube_tls_server_name,
 *   kube_insecure_skip_tls_verify, kube_as_user, kube_as_groups (array,
 *   impersonation), burst_limit, qps, namespace ("default"),
 *   storage_driver ("" = "secret"; also "configmap", "memory", "sql").
 * With neither path nor content, resolution follows the client-go chain:
 * KUBECONFIG env → ~/.kube/config → in-cluster service account (inside a
 * pod). Parsing happens now; the cluster is first contacted by an action.
 * *out receives a handle; free with helm_config_free.
 *
 * Concurrency: one config supports parallel actions on DIFFERENT releases;
 * concurrent writes to the SAME release can corrupt its history — serialize
 * per release. */
int32_t helm_config_new(const char* opts_json, helm_handle_t* out,
                        char** error_out);

/* Frees a config handle (type-checked, idempotent). */
int32_t helm_config_free(helm_handle_t config, char** error_out);

/* Creates a cancellation context for helm_install/helm_upgrade. Free with
 * helm_context_free (freeing also cancels). */
int32_t helm_context_new(helm_handle_t* out, char** error_out);

/* Cancels the context: a blocked install/upgrade using it returns
 * HELM_ERR_CANCELLED. Callable from any thread, repeatedly. */
int32_t helm_context_cancel(helm_handle_t ctx, char** error_out);

/* Cancels and frees a context handle. */
int32_t helm_context_free(helm_handle_t ctx, char** error_out);

/* ------------------------------------------------------------------ */
/* Release actions (cluster)                                           */
/* ------------------------------------------------------------------ */

/* Installs a chart as `name`. The chart comes from EITHER a loaded chart
 * handle (chart != 0, chart_ref NULL) OR a chart reference (chart 0,
 * chart_ref = local path / repo chart name with opts "chart_repo_url" /
 * "oci://host/path/name" — resolved like the helm CLI). ctx optional (0 =
 * not cancellable). values_json optional. opts_json optional; keys:
 * {"namespace":"","timeout_seconds":0,
 * "wait":""|"watcher"|"legacy"|"hookOnly",
 * "dry_run":""|"none"|"client"|"server","create_namespace":false,
 * "rollback_on_failure":false,"description":"","labels":{},
 * "chart_repo_url":"","chart_version":"","plain_http":false}. *out receives
 * the release summary JSON incl. manifest. Blocking: cluster I/O (+ network
 * when resolving a remote chart_ref). */
int32_t helm_install(helm_handle_t config, helm_handle_t ctx,
                     helm_handle_t chart, const char* chart_ref,
                     const char* name, const char* values_json,
                     const char* opts_json, char** out, char** error_out);

/* Upgrades release `name`. Chart selection and parameters as helm_install;
 * extra opts keys: {"max_history":0,"reset_values":false,
 * "reuse_values":false,"cleanup_on_fail":false}. */
int32_t helm_upgrade(helm_handle_t config, helm_handle_t ctx,
                     helm_handle_t chart, const char* chart_ref,
                     const char* name, const char* values_json,
                     const char* opts_json, char** out, char** error_out);

/* Uninstalls release `name`. opts_json keys: {"keep_history":false,
 * "timeout_seconds":0,"dry_run":false,"ignore_not_found":false,"wait":"",
 * "description":""}. *out receives {"info":"...","release":{...}}. */
int32_t helm_uninstall(helm_handle_t config, const char* name,
                       const char* opts_json, char** out, char** error_out);

/* Rolls release `name` back. opts_json keys: {"version":0 (0 = previous),
 * "timeout_seconds":0,"wait":"","dry_run":""}. Status code only. */
int32_t helm_rollback(helm_handle_t config, const char* name,
                      const char* opts_json, char** error_out);

/* Lists releases as a JSON array of summaries (no manifests). opts_json
 * keys: {"all":false,"all_namespaces":false,"limit":0,"offset":0,
 * "filter":"<regex>"}. */
int32_t helm_list(helm_handle_t config, const char* opts_json,
                  char** out, char** error_out);

/* Release summary JSON (incl. manifest) for `name`. opts_json keys:
 * {"revision":0 (0 = latest)}. HELM_ERR_NOT_FOUND if absent. */
int32_t helm_status(helm_handle_t config, const char* name,
                    const char* opts_json, char** out, char** error_out);

/* JSON array of revision summaries for `name`. opts_json keys: {"max":0}. */
int32_t helm_history(helm_handle_t config, const char* name,
                     const char* opts_json, char** out, char** error_out);

/* The release's values as JSON. opts_json keys: {"all":false (true =
 * computed values),"revision":0}. */
int32_t helm_get_values(helm_handle_t config, const char* name,
                        const char* opts_json, char** out, char** error_out);

/* Release metadata JSON (chart, versions, annotations, …) for `name`.
 * opts_json keys: {"revision":0 (0 = latest)}. */
int32_t helm_get_metadata(helm_handle_t config, const char* name,
                          const char* opts_json, char** out, char** error_out);

/* ------------------------------------------------------------------ */
/* Dependencies & provenance                                           */
/* ------------------------------------------------------------------ */

/* Resolves the chart directory's declared dependencies into charts/ and
 * writes Chart.lock (like `helm dependency update`). opts_json keys:
 * {"skip_refresh":false,"keyring":"","verify":false,"plain_http":false}.
 * Uses a private temp cache — never the user's helm config. Blocking:
 * network I/O. */
int32_t helm_dependency_update(const char* chart_dir, const char* opts_json,
                               char** error_out);

/* Rebuilds charts/ from an existing Chart.lock (like `helm dependency
 * build`). Same opts as helm_dependency_update. */
int32_t helm_dependency_build(const char* chart_dir, const char* opts_json,
                              char** error_out);

/* Verifies a chart archive against its provenance signature. prov_file may
 * be NULL (defaults to "<path>.prov"); keyring is a GPG public keyring file.
 * *out receives {"file_name":"...","file_hash":"sha256:...",
 * "signed_by":["identity", ...]} JSON. A bad or missing signature returns
 * HELM_ERR_CHART_INVALID. */
int32_t helm_chart_verify(const char* path, const char* prov_file,
                          const char* keyring, char** out, char** error_out);

/* ------------------------------------------------------------------ */
/* Logging                                                             */
/* ------------------------------------------------------------------ */

typedef enum helm_log_level {
    HELM_LOG_DEBUG = 0,
    HELM_LOG_INFO  = 1,
    HELM_LOG_WARN  = 2,
    HELM_LOG_ERROR = 3
} helm_log_level;

/* Host log callback. `message` is valid ONLY for the duration of the call —
 * copy it before returning. May be invoked concurrently from arbitrary
 * threads; must be thread-safe, fast, and must NOT call back into helm-c. */
typedef void (*helm_log_callback)(int32_t level, const char* message,
                                  void* user_data);

/* Routes helm-c's internal logging (helm SDK log records) to `cb` for every
 * config created AFTER this call. min_level is a helm_log_level. Pass a NULL
 * cb to restore silence (the default). `user_data` is passed through
 * untouched. */
int32_t helm_set_log_handler(helm_log_callback cb, void* user_data,
                             int32_t min_level);

#ifdef __cplusplus
}
#endif

#endif /* HELM_C_H */
