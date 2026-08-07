# helm-c-sdk — C SDK for Helm

**C bindings for the [Helm](https://helm.sh) v4 Go SDK**: a shared library
(`libhelm_c.so` / `libhelm_c.dylib` / `helm_c.dll`) with a small, stable C API, so you can
**use Helm from Node.js, Python, Swift, Rust, C/C++, or any language with FFI** — install,
upgrade, and roll back releases, render and package charts, and talk to OCI registries,
without shelling out to the `helm` binary.

Built on the official Helm Go SDK, pinned to an exact release (`helm.sh/helm/v4 v4.2.3`);
verify at runtime with `helm_sdk_version()`.

## Features

- **Full release lifecycle**: install, upgrade, rollback, uninstall, list, status,
  history, get values/metadata — with wait strategies, timeouts, dry-run, and
  cancellation from any thread
- **Charts offline**: create, load, inspect, lint, package, merge values, validate
  against `values.schema.json`, and render templates — no cluster required
- **Distribution**: OCI registry login/push/pull, HTTP chart repositories, dependency
  update/build, provenance (GPG) verification
- **Every kube connection style**: kubeconfig path or inline content, bearer token,
  impersonation, custom CA/TLS, in-cluster service account
- **Binding-friendly by design**: opaque handles + JSON results (no struct marshalling),
  stable append-only ABI, explicit ownership with idempotent frees safe under GC
  finalizers, a leak probe (`helm_open_handles_count`), and an opt-in log callback
- **Cross-platform**: Linux, macOS, Windows (mingw-w64 gcc and llvm-mingw clang), all
  exercised in CI with race-detector tests, fuzzing, ASan/LSan, and a real-cluster
  (kind) end-to-end job

## Quick start

```bash
make build    # -> build/libhelm_c.{so,dylib} or helm_c.dll + include/helm_c.h
```

```c
#include <stdio.h>
#include "helm_c.h"

int main(void) {
    helm_handle_t chart = 0;
    char *err = NULL, *manifests = NULL;

    if (helm_chart_load("./mychart", &chart, &err) != HELM_OK) {
        fprintf(stderr, "load failed: %s\n", err);
        helm_free_string(err);
        return 1;
    }
    if (helm_render(chart, "{\"replicaCount\":3}", "{\"name\":\"demo\"}",
                    &manifests, &err) == HELM_OK) {
        printf("%s\n", manifests);      /* {"mychart/templates/...": "..."} */
        helm_free_string(manifests);
    }
    helm_chart_free(chart, NULL);
    return 0;
}
```

```bash
cc demo.c -I include -L build -lhelm_c && LD_LIBRARY_PATH=build ./a.out
```

Installing into a cluster is the same pattern:

```c
helm_handle_t cfg = 0;
helm_config_new("{\"namespace\":\"default\"}", &cfg, NULL);  /* uses ~/.kube/config */
char* release = NULL;
helm_install(cfg, 0, chart, NULL, "demo", "{\"replicaCount\":3}", NULL, &release, &err);
```

## Documentation

- [docs/API.md](docs/API.md) — every function: signature, ownership, error codes,
  thread-safety, blocking behavior
- [docs/MEMORY.md](docs/MEMORY.md) — the memory-ownership contract (who allocates, who
  frees), written for binding authors
- [docs/BUILD.md](docs/BUILD.md) — building and linking on Linux/macOS/Windows
- [docs/adr/](docs/adr) — design decisions (handle registry, JSON boundary, error model,
  SDK pinning)

## Scope

This repository ships the **C library, header, and docs only**. Language bindings
(Node.js N-API, Python, Swift, …) are intended to live in their own repositories on top
of this ABI — the API is deliberately shaped so those wrappers stay thin.
