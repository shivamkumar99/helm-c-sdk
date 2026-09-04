GO      ?= go
CC      ?= cc
BUILD   := build

# Library version — baked into filenames and linker metadata so multiple
# versions can coexist side by side. MAJOR bumps on ABI breaks (which the
# append-only rule forbids within a major).
VERSION ?= 0.2.0
MAJOR   := $(word 1,$(subst ., ,$(VERSION)))

ifeq ($(OS),Windows_NT)
  # Windows has no soname mechanism. Build under the plain name — Go's linker
  # writes the output name unquoted into the export .def's LIBRARY line, and
  # GNU ld rejects dotted version names there ("syntax error"; known Go bug).
  # The plain name is also the correct internal/load name; the versioned
  # filename (libhelm_c-$(VERSION).dll) is produced as a copy for distribution.
  LIB      := libhelm_c.dll
  SOFLAGS  :=
  HARNESS  := harness.exe
  PTHREAD  :=
else
  UNAME_S := $(shell uname -s)
  ifeq ($(UNAME_S),Darwin)
    # macOS: versioned dylib + install_name/current_version metadata;
    # libhelm_c.$(MAJOR).dylib and libhelm_c.dylib symlinks alongside.
    LIB     := libhelm_c.$(VERSION).dylib
    SOFLAGS := -extldflags=-Wl,-install_name,@rpath/libhelm_c.$(MAJOR).dylib,-current_version,$(VERSION),-compatibility_version,$(MAJOR).0
  else
    # Linux: classic soname scheme — libhelm_c.so.$(VERSION) with
    # SONAME libhelm_c.so.$(MAJOR), plus the usual symlink chain.
    LIB     := libhelm_c.so.$(VERSION)
    SOFLAGS := -extldflags=-Wl,-soname,libhelm_c.so.$(MAJOR)
  endif
  HARNESS  := harness
  PTHREAD  := -pthread
  # Bake the build dir in as an rpath so the harness finds the library even
  # where the dynamic-loader environment variables are stripped (macOS SIP
  # does this for processes spawned from protected binaries).
  RPATH    := -Wl,-rpath,$(abspath $(BUILD))
endif

.PHONY: all build test vet fixtures harness leak-check pkgconfig clean

all: build

build:
	CGO_ENABLED=1 $(GO) build -buildmode=c-shared -ldflags "$(SOFLAGS)" -o $(BUILD)/$(LIB) ./capi
	@rm -f $(BUILD)/*.h  # cgo-generated header; include/helm_c.h is the shipped one
ifeq ($(OS),Windows_NT)
	cp $(BUILD)/$(LIB) $(BUILD)/libhelm_c-$(VERSION).dll
else ifeq ($(UNAME_S),Darwin)
	ln -sf $(LIB) $(BUILD)/libhelm_c.$(MAJOR).dylib
	ln -sf $(LIB) $(BUILD)/libhelm_c.dylib
else
	ln -sf $(LIB) $(BUILD)/libhelm_c.so.$(MAJOR)
	ln -sf $(LIB) $(BUILD)/libhelm_c.so
endif

vet:
	$(GO) vet ./...

test: vet
	$(GO) test -race ./...

# End-to-end: compile the C harness against the freshly built library and run
# it the way a real binding would.
# Signing fixtures are generated, never committed.
fixtures:
	$(GO) run ./test/genfixtures -dir $(BUILD)/signing

harness: build fixtures
	$(CC) -Wall -Wextra -Werror $(PTHREAD) -o $(BUILD)/$(HARNESS) test/c-harness/*.c \
		-I include -L $(BUILD) -lhelm_c $(RPATH)
	HELMC_SIGNING_DIR=$(BUILD)/signing HELMC_WORK_DIR=$$(mktemp -d) \
		LD_LIBRARY_PATH=$(BUILD) DYLD_LIBRARY_PATH=$(BUILD) ./$(BUILD)/$(HARNESS)

# Linux-only: harness under AddressSanitizer/LeakSanitizer.
leak-check: build fixtures
	$(CC) -Wall -Wextra -Werror $(PTHREAD) -fsanitize=address -o $(BUILD)/$(HARNESS)-asan \
		test/c-harness/*.c -I include -L $(BUILD) -lhelm_c $(RPATH)
	HELMC_SIGNING_DIR=$(BUILD)/signing HELMC_WORK_DIR=$$(mktemp -d) ASAN_OPTIONS=detect_leaks=1 \
		LD_LIBRARY_PATH=$(BUILD) ./$(BUILD)/$(HARNESS)-asan

# Generates a pkg-config file for consumers; VERSION comes from the release
# tag in CI (defaults to the dev version).
VERSION ?= 0.2.0
PREFIX  ?= /usr/local

pkgconfig:
	@mkdir -p $(BUILD)
	@printf 'prefix=%s\nlibdir=$${prefix}/lib\nincludedir=$${prefix}/include\n\nName: helm_c\nDescription: C bindings for the Helm v4 SDK\nVersion: %s\nLibs: -L$${libdir} -lhelm_c\nCflags: -I$${includedir}\n' \
		"$(PREFIX)" "$(VERSION)" > $(BUILD)/helm_c.pc

clean:
	rm -rf $(BUILD)
