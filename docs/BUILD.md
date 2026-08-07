# helm-c — building

## Prerequisites

- Go (version per `go.mod`), with `CGO_ENABLED=1`
- A C toolchain: clang (macOS), gcc (Linux). On **Windows** both supported toolchains
  work and both are exercised in CI: MSYS2 **mingw-w64 gcc** (`choco install mingw` /
  MSYS2 `mingw-w64-x86_64-gcc`) and **llvm-mingw** clang
  (github.com/mstorsjo/llvm-mingw; set `CC=x86_64-w64-mingw32-clang`)
- `make`

## Commands

```bash
make build       # shared library + cgo header into build/
                 #   linux:   build/libhelm_c.so
                 #   macOS:   build/libhelm_c.dylib
                 #   windows: build/libhelm_c.dll
make test        # go vet + go test -race ./...
make harness     # build & run the e2e C harness against the built library
make leak-check  # (linux) harness under AddressSanitizer/LeakSanitizer
make clean
```

The **public** header is `include/helm_c.h` (hand-maintained, documented, stable). The
cgo-generated `build/libhelm_c.h` is an internal artifact — do not ship or include it.

## Consuming the library

```bash
cc myapp.c -I <helm-c>/include -L <helm-c>/build -lhelm_c
# run with the library on the loader path:
#   linux:   LD_LIBRARY_PATH=<helm-c>/build
#   macOS:   DYLD_LIBRARY_PATH=<helm-c>/build
#   windows: PATH=<helm-c>\build;%PATH%
```

## Dependency pin

`go.mod` pins `helm.sh/helm/v4` to an exact release (`v4.2.3`). Verify at runtime with
`helm_sdk_version()`. Upgrading the pin is a standalone change run through the full CI
matrix.

## CI

`.github/workflows/ci.yml` assumes this folder is the repository root and runs, per PR:
build + `-race` tests + C harness on **ubuntu / macos / windows**, golangci-lint, gosec,
govulncheck, and the ASan/LSan leak job; `codeql.yml` runs CodeQL for Go and C. If helm-c
is nested inside another repo, move these workflows to that repo's `.github/` and add
`working-directory` accordingly.
