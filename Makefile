GO      ?= go
CC      ?= cc
BUILD   := build

ifeq ($(OS),Windows_NT)
  LIB      := helm_c.dll
  HARNESS  := harness.exe
else
  UNAME_S := $(shell uname -s)
  ifeq ($(UNAME_S),Darwin)
    LIB    := libhelm_c.dylib
  else
    LIB    := libhelm_c.so
  endif
  HARNESS  := harness
endif

.PHONY: all build test vet harness leak-check pkgconfig clean

all: build

build:
	CGO_ENABLED=1 $(GO) build -buildmode=c-shared -o $(BUILD)/$(LIB) ./capi

vet:
	$(GO) vet ./...

test: vet
	$(GO) test -race ./...

# End-to-end: compile the C harness against the freshly built library and run
# it the way a real binding would.
harness: build
	$(CC) -Wall -Wextra -Werror -o $(BUILD)/$(HARNESS) test/c-harness/main.c \
		-I include -L $(BUILD) -lhelm_c
	HELMC_TESTCHART=testdata/testchart HELMC_KUBECONFIG=testdata/kubeconfig.yaml HELMC_SIGNING_DIR=testdata/signing \
		LD_LIBRARY_PATH=$(BUILD) DYLD_LIBRARY_PATH=$(BUILD) ./$(BUILD)/$(HARNESS)

# Linux-only: harness under AddressSanitizer/LeakSanitizer.
leak-check: build
	$(CC) -Wall -Wextra -Werror -fsanitize=address -o $(BUILD)/$(HARNESS)-asan \
		test/c-harness/main.c -I include -L $(BUILD) -lhelm_c
	HELMC_TESTCHART=testdata/testchart HELMC_KUBECONFIG=testdata/kubeconfig.yaml HELMC_SIGNING_DIR=testdata/signing ASAN_OPTIONS=detect_leaks=1 \
		LD_LIBRARY_PATH=$(BUILD) ./$(BUILD)/$(HARNESS)-asan

# Generates a pkg-config file for consumers; VERSION comes from the release
# tag in CI (defaults to the dev version).
VERSION ?= 0.1.0
PREFIX  ?= /usr/local

pkgconfig:
	@mkdir -p $(BUILD)
	@printf 'prefix=%s\nlibdir=$${prefix}/lib\nincludedir=$${prefix}/include\n\nName: helm_c\nDescription: C bindings for the Helm v4 SDK\nVersion: %s\nLibs: -L$${libdir} -lhelm_c\nCflags: -I$${includedir}\n' \
		"$(PREFIX)" "$(VERSION)" > $(BUILD)/helm_c.pc

clean:
	rm -rf $(BUILD)
