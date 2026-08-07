package main

/*
#include <stdint.h>
*/
import "C"

import (
	"runtime/debug"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
	"github.com/shivamkumar99/helm-c-sdk/internal/handles"
)

// libVersion is the helm-c library version; keep in lockstep with the
// HELM_C_VERSION_* macros in include/helm_c.h.
const libVersion = "0.1.0"

// registry holds every live Go object handed to C as an opaque handle.
var registry = handles.NewRegistry()

// helm_c_version returns the helm-c library version as a malloc'd UTF-8
// string. Caller frees with helm_free_string.
//
//export helm_c_version
func helm_c_version() *C.char {
	return C.CString(libVersion)
}

// helm_sdk_version returns the exact helm.sh/helm/v4 version this library was
// built against (from Go build info). Caller frees with helm_free_string.
//
//export helm_sdk_version
func helm_sdk_version() *C.char {
	return C.CString(sdkVersion())
}

func sdkVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == "helm.sh/helm/v4" {
			if dep.Replace != nil {
				return dep.Replace.Version
			}
			return dep.Version
		}
	}
	return "unknown"
}

// helm_free_string frees any char* previously returned by this library.
// NULL-safe. Each pointer is freed exactly once (see docs/MEMORY.md).
//
//export helm_free_string
func helm_free_string(s *C.char) {
	freeCString(s)
}

// helm_handle_free releases the Go object behind any handle type. Freeing an
// unknown or already-freed handle returns HELM_ERR_INVALID_HANDLE and is
// otherwise harmless (never crashes).
//
//export helm_handle_free
func helm_handle_free(h C.uint64_t, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if err := registry.Free(uint64(h)); err != nil {
		return failure(errOut, err)
	}
	return C.int32_t(cerrors.CodeOK)
}

// helm_open_handles_count reports the number of live handles — the leak probe
// binding test suites assert to be zero at shutdown.
//
//export helm_open_handles_count
func helm_open_handles_count() C.int64_t {
	return C.int64_t(registry.Count())
}
