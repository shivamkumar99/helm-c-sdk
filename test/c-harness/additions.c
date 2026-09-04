#include "harness.h"

/* Chart content accessors on a loaded chart. */
static void additions_chart_content(helm_handle_t h) {
    char* out = NULL;
    CHECK(helm_chart_templates(h, &out, NULL) == HELM_OK && out != NULL &&
              strstr(out, "templates/") != NULL,
          "chart templates listed");
    FREE_AND_NULL(out);
    CHECK(helm_chart_files(h, &out, NULL) == HELM_OK && out != NULL, "chart files listed");
    FREE_AND_NULL(out);
    CHECK(helm_chart_crds(h, &out, NULL) == HELM_OK && out != NULL, "chart crds listed");
    FREE_AND_NULL(out);
    CHECK(helm_chart_schema(h, &out, NULL) == HELM_OK && out != NULL, "chart schema read");
    FREE_AND_NULL(out);
    CHECK(helm_chart_dependencies(h, &out, NULL) == HELM_OK && out != NULL,
          "chart dependencies listed");
    FREE_AND_NULL(out);
}

/* Sign the archive with the generated secret keyring, verify our own
 * signature, and reject signing without key/keyring. */
static void additions_signing(const char* tgz) {
    char* out = NULL;
        const char* sdir = getenv("HELMC_SIGNING_DIR");
    if (sdir != NULL && sdir[0] != '\0') {
        char opts[700];
        snprintf(opts, sizeof(opts),
                 "{\"key\":\"helm-c-sdk-test\",\"keyring\":\"%s/secring.gpg\"}", sdir);
        char* prov = NULL;
        CHECK(helm_chart_sign(tgz, opts, &prov, NULL) == HELM_OK && prov != NULL,
              "archive signed");
        FREE_AND_NULL(prov);
        char keyring[600];
        snprintf(keyring, sizeof(keyring), "%s/pubring.gpg", sdir);
        char* verified = NULL;
        CHECK(helm_chart_verify(tgz, NULL, keyring, &verified, NULL) == HELM_OK,
              "signature we produced verifies");
        FREE_AND_NULL(verified);
    }
    CHECK(helm_chart_sign(tgz, NULL, &out, NULL) == HELM_ERR_INVALID_ARG,
          "signing without key/keyring rejected");
    FREE_AND_NULL(out);
}

/* Load the archive back through the in-memory bytes entry point. */
static void additions_load_from_bytes(const char* tgz) {
    FILE* f = fopen(tgz, "rb");
    if (f != NULL) {
        fseek(f, 0, SEEK_END);
        long n = ftell(f);
        fseek(f, 0, SEEK_SET);
        uint8_t* buf = (n > 0) ? (uint8_t*)malloc((size_t)n) : NULL;
        if (buf != NULL && fread(buf, 1, (size_t)n, f) == (size_t)n) {
            helm_handle_t h2 = 0;
            CHECK(helm_chart_load_archive(buf, (uint64_t)n, &h2, NULL) == HELM_OK,
                  "chart loads from an in-memory archive");
            CHECK(helm_chart_free(h2, NULL) == HELM_OK, "in-memory chart freed");
        }
        free(buf);
        fclose(f);
    }
    helm_handle_t none = 0;
    CHECK(helm_chart_load_archive(NULL, 0, &none, NULL) == HELM_ERR_INVALID_ARG,
          "NULL archive buffer rejected");
}

/* Digest the archive and expand it back into a directory. */
static void additions_digest_and_expand(const char* tgz, const char* workdir) {
    char* digest = NULL;
    CHECK(helm_chart_digest(tgz, &digest, NULL) == HELM_OK && digest != NULL &&
              strncmp(digest, "sha256:", 7) == 0,
          "archive digest computed");
    FREE_AND_NULL(digest);

    char expanded[600];
    snprintf(expanded, sizeof(expanded), "%s/expanded", workdir);
    CHECK(helm_chart_expand(expanded, tgz, NULL) == HELM_OK, "archive expanded");
}

/* Index the directory holding the archive. */
static void additions_repo_index(const char* workdir) {
    char* idx = NULL;
    CHECK(helm_repo_index_generate(workdir, "{\"base_url\":\"https://x/\"}", &idx, NULL)
              == HELM_OK && idx != NULL && strstr(idx, "harnesschart") != NULL,
          "repo index generated");
    FREE_AND_NULL(idx);
}

/* save -> digest -> load from bytes -> expand -> sign/verify -> repo index. */
static void additions_archive_round_trip(helm_handle_t h, const char* workdir) {
    char* tgz = NULL;
    CHECK(helm_chart_save(h, workdir, &tgz, NULL) == HELM_OK && tgz != NULL, "additions: saved");
    if (tgz != NULL) {
        additions_digest_and_expand(tgz, workdir);
        additions_load_from_bytes(tgz);
        additions_signing(tgz);
        additions_repo_index(workdir);
        FREE_AND_NULL(tgz);
    }
}

/* save_dir and create_from. */
static void additions_writers(helm_handle_t h, const char* workdir) {
    char* dir = NULL;
    CHECK(helm_chart_save_dir(h, workdir, &dir, NULL) == HELM_OK && dir != NULL,
          "chart saved as a directory");
    FREE_AND_NULL(dir);
    CHECK(helm_chart_create_from("fromstarter", workdir, g_chart_dir, &dir, NULL) == HELM_OK &&
              dir != NULL,
          "chart created from a starter");
    FREE_AND_NULL(dir);
    CHECK(helm_chart_free(h, NULL) == HELM_OK, "additions: chart freed");
}

/* The --set expression family. */
static void additions_set_family(void) {
    char* out = NULL;
    CHECK(helm_strvals_parse_string("p=80", &out, NULL) == HELM_OK && out != NULL &&
              strstr(out, "\"80\"") != NULL,
          "--set-string keeps strings");
    FREE_AND_NULL(out);
    CHECK(helm_strvals_parse_json("a={\"b\":1}", &out, NULL) == HELM_OK, "--set-json parses");
    FREE_AND_NULL(out);
    CHECK(helm_strvals_parse_literal("a=b,c", &out, NULL) == HELM_OK, "--set-literal parses");
    FREE_AND_NULL(out);
    CHECK(helm_strvals_parse_file("k=/no/such/file", &out, NULL) == HELM_ERR_VALUES,
          "--set-file with a missing file is a defined error");
    FREE_AND_NULL(out);
}

/* YAML values, show, lint options, the --set family, dependency list. */
static void additions_values_show_lint_set(void) {
    char* out = NULL;
    CHECK(helm_values_from_yaml("a: 1\n", &out, NULL) == HELM_OK && out != NULL &&
              strstr(out, "\"a\":1") != NULL,
          "YAML values parsed");
    FREE_AND_NULL(out);
    CHECK(helm_show(0, g_chart_dir, "{\"format\":\"chart\"}", &out, NULL) == HELM_OK &&
              out != NULL && strstr(out, "harnesschart") != NULL,
          "show renders the chart definition");
    FREE_AND_NULL(out);
    CHECK(helm_lint_run_opts(g_chart_dir, NULL, "{\"strict\":true}", &out, NULL) == HELM_OK &&
              out != NULL,
          "lint with options runs");
    FREE_AND_NULL(out);
    additions_set_family();
    CHECK(helm_dependency_list(g_chart_dir, &out, NULL) == HELM_OK && out != NULL,
          "dependency list runs");
    FREE_AND_NULL(out);
}

/* Registry queries against an unreachable host: defined errors. */
static void additions_registry_queries(void) {
    char* out = NULL;
    /* Registry queries against an unreachable host: defined errors. */
    helm_handle_t rc = 0;
    CHECK(helm_registry_client_new("{\"plain_http\":true}", &rc, NULL) == HELM_OK,
          "client for registry queries");
    out = NULL;
    CHECK(helm_registry_tags(rc, "oci://127.0.0.1:1/x/y", &out, NULL) == HELM_ERR_REGISTRY,
          "tags against unreachable registry is a defined error");
    FREE_AND_NULL(out);
    CHECK(helm_registry_resolve(rc, "oci://127.0.0.1:1/x/y:1.0.0", &out, NULL) == HELM_ERR_REGISTRY,
          "resolve against unreachable registry is a defined error");
    FREE_AND_NULL(out);
    CHECK(helm_registry_client_free(rc, NULL) == HELM_OK, "query client freed");
}

/* Symbols added after 0.1.0 that run offline. Uses the chart authored earlier
 * in the run. */
void test_additions(void) {
    const char* workdir = getenv("HELMC_WORK_DIR");
    if (g_chart_dir == NULL || workdir == NULL || workdir[0] == '\0') {
        printf("skip: no authored chart / HELMC_WORK_DIR — additions not exercised\n");
        return;
    }
    helm_handle_t h = 0;
    CHECK(helm_chart_load(g_chart_dir, &h, NULL) == HELM_OK, "additions: chart loads");
    additions_chart_content(h);
    additions_archive_round_trip(h, workdir);
    additions_writers(h, workdir);
    additions_values_show_lint_set();
    additions_registry_queries();
}

/* Additions since 0.1.0 that need a config handle: reachability probe,
 * registry-client binding, get/test against an unreachable cluster, and a
 * cluster-aware render. Called from test_config_and_context. */
void config_additions_checks(helm_handle_t cfg) {
        CHECK(helm_config_check_reachable(cfg, NULL) == HELM_ERR_KUBE,
          "reachability probe reports the unreachable cluster");
    helm_handle_t rc = 0;
    CHECK(helm_registry_client_new(NULL, &rc, NULL) == HELM_OK, "client for binding");
    CHECK(helm_config_set_registry_client(cfg, rc, NULL) == HELM_OK,
          "registry client bound to config");
    CHECK(helm_config_set_registry_client(cfg, 0, NULL) == HELM_OK,
          "registry client unbound with 0");
    CHECK(helm_registry_client_free(rc, NULL) == HELM_OK, "binding client freed");
    char* got = NULL;
    CHECK(helm_get(cfg, "absent", NULL, &got, NULL) != HELM_OK,
          "get against unreachable cluster is a defined error");
    helm_free_string(got);
    CHECK(helm_test_run(cfg, "absent", NULL, &got, NULL) != HELM_OK,
          "test against unreachable cluster is a defined error");
    helm_free_string(got);
    if (g_chart_dir != NULL) {
        helm_handle_t ch = 0;
        if (helm_chart_load(g_chart_dir, &ch, NULL) == HELM_OK) {
            /* Only `lookup` touches the cluster; the authored chart never
             * calls it, so a cluster-aware render still succeeds. */
            char* rendered = NULL;
            CHECK(helm_render_with_config(cfg, ch, NULL, "{\"name\":\"cfg-r\"}",
                                          &rendered, NULL) == HELM_OK,
                  "cluster-aware render succeeds without lookup");
            CHECK(rendered != NULL && strstr(rendered, "cfg-r") != NULL,
                  "cluster-aware render carries the release name");
            helm_free_string(rendered);
            helm_chart_free(ch, NULL);
        }
    }
}
