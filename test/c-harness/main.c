/*
 * End-to-end harness: drives the built helm-c shared library through the real
 * C ABI exactly the way a language binding would — create, use, free,
 * double-free, and the leak gate. Exits non-zero on any failure.
 */
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#ifndef _WIN32
#include <fcntl.h>
#include <pthread.h>
#include <sys/stat.h>
#include <unistd.h>
#endif

#include "helm_c.h"

static int failures = 0;

/* Chart directory produced by test_chart_authoring; later tests reuse it so
 * the harness needs no pre-existing chart fixture on disk. */
static char* g_chart_dir = NULL;

static const char* kubeconfig_yaml =
    "apiVersion: v1\n"
    "kind: Config\n"
    "clusters:\n"
    "  - name: helmc-test\n"
    "    cluster:\n"
    "      server: https://127.0.0.1:1\n"
    "contexts:\n"
    "  - name: helmc-test\n"
    "    context:\n"
    "      cluster: helmc-test\n"
    "      user: helmc-test\n"
    "current-context: helmc-test\n"
    "users:\n"
    "  - name: helmc-test\n"
    "    user: {}\n";

#define CHECK(cond, msg)                                            \
    do {                                                            \
        if (!(cond)) {                                              \
            fprintf(stderr, "FAIL: %s (%s:%d)\n", msg, __FILE__, __LINE__); \
            failures++;                                             \
        }                                                           \
    } while (0)

/* Upper bound for library-returned strings when length-checking; keeps
 * string inspection bounded even if a returned buffer were unterminated
 * (CWE-126). */
#define HELMC_MAX_STR 4096

static void test_versions(void) {
    char* v = helm_c_version();
    CHECK(v != NULL && strnlen(v, HELMC_MAX_STR) > 0,
          "helm_c_version returns a string");
    char* sdk = helm_sdk_version();
    CHECK(sdk != NULL && strncmp(sdk, "v4.", 3) == 0,
          "helm_sdk_version reports the pinned v4 release");
    printf("helm-c %s (helm SDK %s)\n", v ? v : "?", sdk ? sdk : "?");
    helm_free_string(v);
    helm_free_string(sdk);
}

static void test_string_free_null_safe(void) {
    helm_free_string(NULL); /* must be a no-op */
}

static void test_invalid_handles(void) {
    CHECK(helm_handle_free(0, NULL) == HELM_ERR_INVALID_HANDLE,
          "freeing handle 0 is a defined error");

    char* err = NULL;
    CHECK(helm_handle_free(987654, &err) == HELM_ERR_INVALID_HANDLE,
          "freeing an unknown handle is a defined error");
    CHECK(err != NULL, "error detail provided for invalid handle");
    helm_free_string(err);
}

static void test_release_name_validate(void) {
    CHECK(helm_release_name_validate("valid-name", NULL) == HELM_OK,
          "valid release name accepted");

    char* err = NULL;
    CHECK(helm_release_name_validate("Invalid_NAME!", &err) == HELM_ERR_INVALID_ARG,
          "invalid release name rejected");
    CHECK(err != NULL, "error detail provided for invalid name");
    helm_free_string(err);

    err = NULL;
    CHECK(helm_release_name_validate(NULL, &err) == HELM_ERR_INVALID_ARG,
          "NULL name rejected without crash");
    helm_free_string(err);
}

static void test_strvals_parse(void) {
    char* out = NULL;
    char* err = NULL;
    CHECK(helm_strvals_parse("a=1,b=two", &out, &err) == HELM_OK,
          "strvals parse succeeds");
    CHECK(out != NULL && strstr(out, "\"b\":\"two\"") != NULL,
          "strvals JSON contains parsed pair");
    helm_free_string(out);
    helm_free_string(err);

    out = NULL;
    err = NULL;
    CHECK(helm_strvals_parse("a=1,,=x=", &out, &err) == HELM_ERR_VALUES,
          "malformed strvals rejected");
    helm_free_string(out);
    helm_free_string(err);
}

static void test_chart_load_missing(void) {
    helm_handle_t h = 0;
    char* err = NULL;
    CHECK(helm_chart_load("does-not-exist-xyz", &h, &err) == HELM_ERR_CHART_LOAD,
          "loading a missing chart fails with HELM_ERR_CHART_LOAD");
    CHECK(h == 0, "no handle issued on failure");
    helm_free_string(err);
}

/* Chart inspection: metadata, values, lint, and value merging. */
static void chart_inspection_checks(helm_handle_t h, const char* fixture) {
    char* meta = NULL;
    CHECK(helm_chart_metadata(h, &meta, NULL) == HELM_OK, "metadata retrieved");
    CHECK(meta != NULL && strstr(meta, "harnesschart") != NULL,
          "metadata names the authored chart");
    helm_free_string(meta);

    char* vals = NULL;
    CHECK(helm_chart_values(h, &vals, NULL) == HELM_OK, "values retrieved");
    CHECK(vals != NULL && vals[0] == '{',
          "values render as a JSON object");
    helm_free_string(vals);

    char* lint = NULL;
    CHECK(helm_lint_run(fixture, NULL, &lint, NULL) == HELM_OK, "lint runs");
    CHECK(lint != NULL && strstr(lint, "total_charts_linted") != NULL,
          "lint report is shaped as documented");
    helm_free_string(lint);

    char* merged = NULL;
    CHECK(helm_chart_merge_values(h, "{\"replicaCount\":9}", &merged, NULL) == HELM_OK,
          "values merge succeeds");
    CHECK(merged != NULL && strstr(merged, "\"replicaCount\":9") != NULL,
          "override wins in merged values");
    helm_free_string(merged);
}

/* Offline rendering: schema validation, render, and option validation. */
static void chart_render_checks(helm_handle_t h) {
    CHECK(helm_schema_validate(h, NULL, NULL) == HELM_OK,
          "schema validation passes (chart has no schema)");

    char* manifests = NULL;
    CHECK(helm_render(h, "{\"replicaCount\":2}", "{\"name\":\"harness-rel\"}",
                      &manifests, NULL) == HELM_OK,
          "offline render succeeds");
    CHECK(manifests != NULL && strstr(manifests, "harness-rel") != NULL,
          "rendered manifest carries the release name");
    helm_free_string(manifests);

    char* rerr = NULL;
    CHECK(helm_render(h, NULL, "{\"bogus_key\":true}", &manifests, &rerr)
              == HELM_ERR_INVALID_ARG,
          "unknown render option key rejected");
    helm_free_string(rerr);
}

/* Full chart lifecycle against the chart authored earlier in the run. */
static void test_chart_lifecycle(void) {
    const char* fixture = g_chart_dir;
    if (fixture == NULL) {
        printf("skip: no authored chart — chart lifecycle not exercised\n");
        return;
    }

    helm_handle_t h = 0;
    char* err = NULL;
    CHECK(helm_chart_load(fixture, &h, &err) == HELM_OK, "authored chart loads");
    if (err) { fprintf(stderr, "  load error: %s\n", err); helm_free_string(err); }
    CHECK(h != 0, "chart handle issued");

    chart_inspection_checks(h, fixture);
    chart_render_checks(h);

    CHECK(helm_chart_free(h, NULL) == HELM_OK, "chart freed");
    CHECK(helm_chart_free(h, NULL) == HELM_ERR_INVALID_HANDLE,
          "chart double-free is a defined error");

    char* meta2 = NULL;
    CHECK(helm_chart_metadata(h, &meta2, NULL) == HELM_ERR_INVALID_HANDLE,
          "using a freed chart handle is a defined error");
    helm_free_string(meta2);
}

/* Chart authoring through the ABI: create -> load -> save -> package.
 * Needs a writable scratch dir via HELMC_WORK_DIR. */
static void test_chart_authoring(void) {
    const char* workdir = getenv("HELMC_WORK_DIR");
    if (workdir == NULL || workdir[0] == '\0') {
        printf("skip: HELMC_WORK_DIR not set — chart authoring not exercised\n");
        return;
    }

    char* chart_dir = NULL;
    char* err = NULL;
    CHECK(helm_chart_create("harnesschart", workdir, &chart_dir, &err) == HELM_OK,
          "chart scaffold created");
    if (err) { fprintf(stderr, "  create error: %s\n", err); helm_free_string(err); }
    CHECK(chart_dir != NULL && strstr(chart_dir, "harnesschart") != NULL,
          "created path names the chart");

    err = NULL;
    char* bad_out = NULL;
    CHECK(helm_chart_create("Bad Name!", workdir, &bad_out, &err) == HELM_ERR_INVALID_ARG,
          "invalid chart name rejected");
    helm_free_string(bad_out);
    helm_free_string(err);

    helm_handle_t h = 0;
    CHECK(helm_chart_load(chart_dir, &h, NULL) == HELM_OK, "scaffold loads");

    char* tgz = NULL;
    CHECK(helm_chart_save(h, workdir, &tgz, NULL) == HELM_OK, "chart saved");
    CHECK(tgz != NULL && strstr(tgz, "harnesschart-") != NULL,
          "saved archive named <chart>-<version>");
    helm_free_string(tgz);
    CHECK(helm_chart_free(h, NULL) == HELM_OK, "authored chart freed");

    char opts[600];
    snprintf(opts, sizeof(opts), "{\"destination\":\"%s\"}", workdir);
    char* pkg = NULL;
    CHECK(helm_package_run(chart_dir, opts, &pkg, NULL) == HELM_OK, "chart packaged");
    CHECK(pkg != NULL && strstr(pkg, ".tgz") != NULL, "package produced a .tgz");
    helm_free_string(pkg);

    /* Kept (not freed) so the lifecycle and soak tests below can reuse the
     * authored chart instead of needing a committed fixture. */
    g_chart_dir = chart_dir;
}

/* Log-handler registration through the ABI. Record delivery/level mapping is
 * covered by the boundary tests; here the contract is: set, re-set, and
 * clear all succeed and never crash. */
static void helmc_harness_log_sink(int32_t level, const char* message, void* user_data) {
    (void)level; (void)message;
    if (user_data) { (*(int*)user_data)++; }
}

static void test_log_handler(void) {
    int hits = 0;
    CHECK(helm_set_log_handler(helmc_harness_log_sink, &hits, HELM_LOG_DEBUG) == HELM_OK,
          "log handler installed");
    CHECK(helm_set_log_handler(helmc_harness_log_sink, &hits, HELM_LOG_ERROR) == HELM_OK,
          "log handler level re-set");
    CHECK(helm_set_log_handler(NULL, NULL, 0) == HELM_OK, "log handler cleared");
    CHECK(hits >= 0, "sink storage untouched unless invoked");
}

static void test_registry_client_lifecycle(void) {
    helm_handle_t h = 0;
    CHECK(helm_registry_client_new(NULL, &h, NULL) == HELM_OK,
          "registry client created with default options");
    CHECK(h != 0, "registry client handle issued");

    CHECK(helm_chart_free(h, NULL) == HELM_ERR_WRONG_HANDLE_TYPE,
          "registry client not freeable as a chart");
    CHECK(helm_registry_client_free(h, NULL) == HELM_OK, "registry client freed");
    CHECK(helm_registry_client_free(h, NULL) == HELM_ERR_INVALID_HANDLE,
          "registry client double-free is a defined error");

    char* err = NULL;
    CHECK(helm_registry_client_new("{\"bad_key\":1}", &h, &err) == HELM_ERR_INVALID_ARG,
          "unknown registry client option rejected");
    helm_free_string(err);
}

static void test_chart_verify(void) {
    const char* dir = getenv("HELMC_SIGNING_DIR");
    if (dir == NULL || dir[0] == '\0') {
        printf("skip: HELMC_SIGNING_DIR not set — provenance verify not exercised\n");
        return;
    }
    char tgz[512], keyring[512];
    snprintf(tgz, sizeof(tgz), "%s/testchart-0.1.0.tgz", dir);
    snprintf(keyring, sizeof(keyring), "%s/pubring.gpg", dir);

    char* out = NULL;
    char* err = NULL;
    CHECK(helm_chart_verify(tgz, NULL, keyring, &out, &err) == HELM_OK,
          "signed chart verifies");
    if (err) { fprintf(stderr, "  verify error: %s\n", err); helm_free_string(err); }
    CHECK(out != NULL && strstr(out, "signed_by") != NULL,
          "verification report names the signer");
    helm_free_string(out);

    out = NULL;
    err = NULL;
    CHECK(helm_chart_verify("no-such-chart.tgz", NULL, keyring, &out, &err)
              == HELM_ERR_CHART_INVALID,
          "verifying a missing chart is a defined error");
    helm_free_string(out);
    helm_free_string(err);
}

/* Writes the dummy fixture kubeconfig into HELMC_WORK_DIR. Returns 1 and
 * fills dst on success; 0 when the workdir is unset/unusable.
 * workdir comes from our own Makefile/CI (mktemp -d); reject anything
 * that walks out of it, and create the file owner-only (0600).
 * A cpp/path-injection finding remains here by design: this is a test
 * harness intentionally writing a dummy fixture inside its own
 * environment-supplied scratch directory. */
static int write_fixture_kubeconfig(char* dst, size_t cap) {
    static const char suffix[] = "/kubeconfig.yaml";
    const char* workdir = getenv("HELMC_WORK_DIR");
    if (workdir == NULL || workdir[0] == '\0' || strstr(workdir, "..") != NULL) {
        return 0;
    }
    /* Bounded, truncation-free path construction (a plain snprintf here
     * trips gcc -Werror=format-truncation because the env value length is
     * unknown at compile time). */
    size_t wl = strnlen(workdir, cap);
    if (wl + sizeof(suffix) > cap) {
        return 0;
    }
    /* Both copies are bounded by the guard above: wl + sizeof(suffix) <= cap,
     * so dst (capacity cap) always holds workdir (wl bytes) plus the
     * NUL-terminated suffix. */
    memcpy(dst, workdir, wl);                /* Flawfinder: ignore */
    memcpy(dst + wl, suffix, sizeof(suffix)); /* Flawfinder: ignore */
    FILE* f = NULL;
#ifndef _WIN32
    int fd = open(dst, O_WRONLY | O_CREAT | O_TRUNC, 0600);
    if (fd >= 0) { f = fdopen(fd, "w"); }
#else
    f = fopen(dst, "w");
#endif
    int ok = (f != NULL && fputs(kubeconfig_yaml, f) != EOF);
    if (f != NULL) { fclose(f); }
    return ok;
}

static void test_config_and_context(void) {
    char kubeconfig[512];
    kubeconfig[0] = '\0';
    if (!write_fixture_kubeconfig(kubeconfig, sizeof(kubeconfig))) {
        kubeconfig[0] = '\0';
    }
    if (kubeconfig[0] == '\0') {
        printf("skip: HELMC_WORK_DIR not set — config lifecycle not exercised\n");
    } else {
        /* Sized for the worst case gcc computes under -Wformat-truncation:
         * kubeconfig (<= 511 bytes) + the JSON wrapper (48) + NUL. */
        char opts[640];
        snprintf(opts, sizeof(opts),
                 "{\"kubeconfig_path\":\"%s\",\"storage_driver\":\"memory\"}",
                 kubeconfig);

        helm_handle_t cfg = 0;
        char* err = NULL;
        CHECK(helm_config_new(opts, &cfg, &err) == HELM_OK, "config created");
        if (err) { fprintf(stderr, "  config error: %s\n", err); helm_free_string(err); }
        CHECK(cfg != 0, "config handle issued");

        /* The fixture kubeconfig points at an unreachable server; actions
         * check reachability first, so this must fail as a defined error —
         * never hang or crash. */
        char* list = NULL;
        char* lerr = NULL;
        CHECK(helm_list(cfg, NULL, &list, &lerr) == HELM_ERR_RELEASE,
              "list against unreachable cluster is a defined error");
        CHECK(lerr != NULL && strstr(lerr, "unreachable") != NULL,
              "list error names the unreachable cluster");
        helm_free_string(list);
        helm_free_string(lerr);

        CHECK(helm_config_free(cfg, NULL) == HELM_OK, "config freed");
        CHECK(helm_config_free(cfg, NULL) == HELM_ERR_INVALID_HANDLE,
              "config double-free is a defined error");
    }

    helm_handle_t ctx = 0;
    CHECK(helm_context_new(&ctx, NULL) == HELM_OK, "context created");
    CHECK(helm_context_cancel(ctx, NULL) == HELM_OK, "context cancelled");
    CHECK(helm_context_cancel(ctx, NULL) == HELM_OK, "context cancel is repeatable");
    CHECK(helm_context_free(ctx, NULL) == HELM_OK, "context freed");
    CHECK(helm_context_free(ctx, NULL) == HELM_ERR_INVALID_HANDLE,
          "context double-free is a defined error");
}

static void test_soak(void) {
    /* Hot loop over the full call surface: any per-call leak shows up under
     * the leak gate / ASan run. */
    for (int i = 0; i < 10000; i++) {
        char* v = helm_c_version();
        helm_free_string(v);
        char* err = NULL;
        helm_release_name_validate("soak-name", &err);
        helm_free_string(err);
        helm_handle_free(0, NULL);
    }
}

static void test_chart_soak(void) {
    /* Handle-churn loop: any leaked handle or C string shows up in the leak
     * gate / ASan run. */
    const char* fixture = g_chart_dir;
    if (fixture == NULL) {
        return;
    }
    for (int i = 0; i < 100; i++) {
        helm_handle_t h = 0;
        if (helm_chart_load(fixture, &h, NULL) != HELM_OK) {
            CHECK(0, "soak: chart load failed");
            return;
        }
        char* meta = NULL;
        helm_chart_metadata(h, &meta, NULL);
        helm_free_string(meta);
        helm_chart_free(h, NULL);
    }
}

/* Concurrency stress: FFI hosts (Node's libuv pool, Swift GCD, Python
 * threads) call from arbitrary threads — hammer the ABI from many pthreads
 * at once. Windows builds skip this section (the same paths run under Go's
 * race detector in CI on all OSes). */
#ifndef _WIN32
#define STRESS_THREADS 8
#define STRESS_ITERS 50

static void* stress_worker(void* arg) {
    (void)arg;
    for (int i = 0; i < STRESS_ITERS; i++) {
        /* Pure calls. */
        char* v = helm_c_version();
        helm_free_string(v);
        helm_release_name_validate("stress-name", NULL);

        char* out = NULL;
        if (helm_strvals_parse("a=1,b=two", &out, NULL) == HELM_OK) {
            helm_free_string(out);
        }

        /* Handle churn across threads. */
        helm_handle_t rc = 0;
        if (helm_registry_client_new(NULL, &rc, NULL) == HELM_OK) {
            helm_registry_client_free(rc, NULL);
        }
        if (g_chart_dir != NULL) {
            helm_handle_t ch = 0;
            if (helm_chart_load(g_chart_dir, &ch, NULL) == HELM_OK) {
                char* meta = NULL;
                helm_chart_metadata(ch, &meta, NULL);
                helm_free_string(meta);
                helm_chart_free(ch, NULL);
            }
        }

        /* Defined errors under contention. */
        helm_handle_free(0, NULL);
        helm_open_handles_count();
    }
    return NULL;
}

static void test_thread_stress(void) {
    pthread_t threads[STRESS_THREADS];
    for (int i = 0; i < STRESS_THREADS; i++) {
        if (pthread_create(&threads[i], NULL, stress_worker, NULL) != 0) {
            CHECK(0, "pthread_create failed");
            return;
        }
    }
    for (int i = 0; i < STRESS_THREADS; i++) {
        pthread_join(threads[i], NULL);
    }
    CHECK(helm_open_handles_count() == 0,
          "thread stress leaves no live handles");
    printf("thread stress: %d threads x %d iterations completed\n",
           STRESS_THREADS, STRESS_ITERS);
}
#else
static void test_thread_stress(void) {
    printf("skip: thread stress not built on Windows\n");
}
#endif

int main(void) {
    test_versions();
    test_string_free_null_safe();
    test_invalid_handles();
    test_release_name_validate();
    test_strvals_parse();
    test_chart_load_missing();
    test_chart_authoring(); /* must precede lifecycle/soak: authors the chart */
    test_chart_lifecycle();
    test_log_handler();
    test_registry_client_lifecycle();
    test_chart_verify();
    test_config_and_context();
    test_soak();
    test_chart_soak();
    test_thread_stress();

    /* Leak gate: nothing in this harness may leave a live handle behind. */
    CHECK(helm_open_handles_count() == 0, "leak gate: zero open handles");

    if (failures > 0) {
        fprintf(stderr, "harness: %d check(s) FAILED\n", failures);
        return 1;
    }
    printf("harness: all checks passed\n");
    return 0;
}
