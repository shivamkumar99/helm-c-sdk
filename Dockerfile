# syntax=docker/dockerfile:1

# ---------------------------------------------------------------------------
# Stage 1 — builder: compile the cgo shared library and the C harness.
# Pinned toolchain image matching go.mod; Debian 12 glibc matches the
# distroless runtime below.
# ---------------------------------------------------------------------------
FROM golang:1.27-bookworm AS builder

WORKDIR /src

# Dependency layer first for build caching.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# -trimpath keeps build-host paths out of the binary.
RUN CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -o /out/libhelm_c.so ./capi \
 && gcc -Wall -Wextra -Werror -o /out/helm-c-harness test/c-harness/*.c \
      -I include -L /out -lhelm_c

# ---------------------------------------------------------------------------
# Stage 2 — test gate: the image fails to build unless vet, the unit tests,
# and the end-to-end C harness all pass.
# ---------------------------------------------------------------------------
FROM builder AS test

RUN go vet ./... \
 && go test ./internal/... ./pkg/cerrors/... ./capi/... \
 && go run ./test/genfixtures -dir /tmp/signing \
 && HELMC_SIGNING_DIR=/tmp/signing \
    HELMC_WORK_DIR=/tmp/work \
    LD_LIBRARY_PATH=/out sh -c 'mkdir -p /tmp/work && /out/helm-c-harness' 

# ---------------------------------------------------------------------------
# Stage 3 — runtime: hardened, minimal.
#   - distroless: no shell, no package manager, minimal CVE surface
#   - runs as the non-root "nonroot" user
#   - contains only the library, header, and smoke-test harness
# Also usable as an artifact image:
#   docker create --name x <image> && docker cp x:/usr/local/lib/libhelm_c.so .
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/base-debian12:nonroot AS runtime

LABEL org.opencontainers.image.title="helm-c-sdk" \
      org.opencontainers.image.description="C SDK for Helm v4 — shared library exposing chart, registry, and release operations over a stable C ABI" \
      org.opencontainers.image.source="https://github.com/shivamkumar99/helm-c-sdk" \
      org.opencontainers.image.licenses="Apache-2.0"

# COPY --from=test (not builder) so the test gate cannot be skipped by cache.
COPY --from=test /out/libhelm_c.so /usr/local/lib/libhelm_c.so
COPY --from=test /out/helm-c-harness /usr/local/bin/helm-c-harness
COPY --from=test /src/include/helm_c.h /usr/local/include/helm_c.h
COPY --from=test /src/LICENSE /src/NOTICE /usr/share/doc/helm-c-sdk/

ENV LD_LIBRARY_PATH=/usr/local/lib

USER nonroot:nonroot

# Default command: the library's self-check (versions, error paths, leak
# gate). Runs entirely offline and needs no privileges.
ENTRYPOINT ["/usr/local/bin/helm-c-harness"]
