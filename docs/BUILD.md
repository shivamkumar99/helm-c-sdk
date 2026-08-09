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
make build       # versioned shared library into build/ (VERSION=x.y.z, default 0.1.0)
                 #   linux:   libhelm_c.so.<version>  (SONAME libhelm_c.so.<major>)
                 #            + symlinks libhelm_c.so.<major>, libhelm_c.so
                 #   macOS:   libhelm_c.<version>.dylib (install_name
                 #            @rpath/libhelm_c.<major>.dylib, current_version set)
                 #            + symlinks libhelm_c.<major>.dylib, libhelm_c.dylib
                 #   windows: libhelm_c.dll (built + internal name) with a
                 #   versioned libhelm_c-<version>.dll copy for distribution
                 # Multiple versions install side by side; link with -lhelm_c via
                 # the unversioned name, load at runtime via the major-versioned one.
make test        # go vet + go test -race ./...
make harness     # build & run the e2e C harness against the built library
make leak-check  # (linux) harness under AddressSanitizer/LeakSanitizer
make pkgconfig   # generate build/helm_c.pc (VERSION=x.y.z PREFIX=/usr/local)
make clean
```

The **public** header is `include/helm_c.h` (hand-maintained, documented, stable). The
cgo-generated header is an internal artifact and is deleted automatically by `make build`.

## Consuming the library

```bash
cc myapp.c -I <helm-c>/include -L <helm-c>/build -lhelm_c
# run with the library on the loader path:
#   linux:   LD_LIBRARY_PATH=<helm-c>/build
#   macOS:   DYLD_LIBRARY_PATH=<helm-c>/build
#   windows: PATH=<helm-c>\build;%PATH%
```

## Release downloads

Each tagged release attaches, per platform, both a full tarball
(`helm-c-<version>-<platform>.tar.gz`: library + `helm_c.h` + pkg-config file +
docs + LICENSE/NOTICE) and **standalone files for direct download** —
`libhelm_c-<version>-linux-amd64.so`, `libhelm_c-<version>-darwin-arm64.dylib`,
`libhelm_c-<version>-windows-amd64.dll`, and `helm_c.h` — plus
`sha256sums.txt` (covering every asset), an SPDX SBOM, and the ClamAV scan log.
To consume the library you need exactly two files: the platform's library and
`helm_c.h`.

## Building for a platform without a release

`make build` works on every platform Go supports, so an unsupported architecture or libc
is only a `make build` away — there is nothing platform-specific in this repository beyond
the C toolchain requirement. Consumers then load that artifact directly (bindings accept a
path; e.g. `HELM_C_LIB` for the Python binding) rather than relying on the OS loader path.

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
