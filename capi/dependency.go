package main

/*
#include <stdint.h>
*/
import "C"

import (
	"github.com/shivamkumar99/helm-c-sdk/pkg/wrapper"
)

// chartDirAction is the shared shim body for chart_dir + opts → status calls.
func chartDirAction(chartDir *C.char, optsJSON *C.char, errOut **C.char,
	run func(dir string, opts wrapper.DependencyOptions) error) C.int32_t {
	return statusResult(errOut, func() error {
		if err := requireArgs("chart_dir", chartDir); err != nil {
			return err
		}
		opts, err := wrapper.ParseDependencyOptions(optionalGoString(optsJSON))
		if err != nil {
			return err
		}
		return run(C.GoString(chartDir), opts)
	})
}

// helm_dependency_update resolves the chart's declared dependencies into
// charts/ and writes Chart.lock. opts_json optional — see docs/API.md.
// Blocking: network I/O.
//
//export helm_dependency_update
func helm_dependency_update(chartDir *C.char, optsJSON *C.char, errOut **C.char) C.int32_t {
	return chartDirAction(chartDir, optsJSON, errOut, wrapper.DependencyUpdate)
}

// helm_dependency_build rebuilds charts/ from an existing Chart.lock.
//
//export helm_dependency_build
func helm_dependency_build(chartDir *C.char, optsJSON *C.char, errOut **C.char) C.int32_t {
	return chartDirAction(chartDir, optsJSON, errOut, wrapper.DependencyBuild)
}

// helm_chart_verify checks a chart archive against its provenance file with
// a GPG public keyring. prov_file may be NULL (defaults to <path>.prov).
// *out receives {"file_name","file_hash","signed_by":[...]} JSON.
//
//export helm_chart_verify
func helm_chart_verify(path *C.char, provFile *C.char, keyring *C.char, out **C.char, errOut **C.char) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("path and keyring", path, keyring); err != nil {
			return "", err
		}
		return wrapper.VerifyChart(C.GoString(path), optionalGoString(provFile), C.GoString(keyring))
	})
}

// helm_get_metadata returns release metadata JSON for `name` (opts key
// "revision"; 0 = latest).
//
//export helm_get_metadata
func helm_get_metadata(cfgH C.uint64_t, name *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return releaseAction(releaseShimArgs{cfgH, name, optsJSON, out, errOut}, func(cfgObj any, name, optsJSON string) (string, error) {
		opts, err := wrapper.ParseGetMetadataOptions(optsJSON)
		if err != nil {
			return "", err
		}
		return wrapper.GetReleaseMetadata(cfgObj, name, opts)
	})
}
