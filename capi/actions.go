package main

/*
#include <stdint.h>
*/
import "C"

import (
	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
	"github.com/shivamkumar99/helm-c-sdk/internal/handles"
	"github.com/shivamkumar99/helm-c-sdk/internal/wrapper"
)

// configAndOptionalChart resolves the config handle plus the chart handle
// when one is given (0 = the shim will resolve a chart_ref instead).
func configAndOptionalChart(cfgH, chartH C.uint64_t) (cfgObj, chartObj any, err error) {
	cfgObj, err = registry.Get(uint64(cfgH), handles.TypeConfig)
	if err != nil {
		return nil, nil, err
	}
	if chartH != 0 {
		chartObj, err = registry.Get(uint64(chartH), handles.TypeChart)
		if err != nil {
			return nil, nil, err
		}
	}
	return cfgObj, chartObj, nil
}

// helm_install installs a chart as `name`. The chart comes from EITHER a
// loaded chart handle (chart != 0, chart_ref NULL) OR a chart reference
// (chart 0, chart_ref = local path, repo chart name with opts
// "chart_repo_url", or an oci:// URL). ctx is optional (0 = no cancellation).
// values_json and opts_json optional. *out receives the release summary JSON
// (with manifest). Blocking: cluster and possibly network I/O.
//
//export helm_install
func helm_install(cfgH, ctxH, chartH C.uint64_t, chartRef *C.char, name *C.char, valuesJSON *C.char, optsJSON *C.char, out **C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if name == nil || out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "name and out must not be NULL"))
	}
	cfgObj, chartObj, err := configAndOptionalChart(cfgH, chartH)
	if err != nil {
		return failure(errOut, err)
	}
	ctx, err := ctxOrBackground(ctxH)
	if err != nil {
		return failure(errOut, err)
	}
	opts, err := wrapper.ParseInstallOptions(optionalGoString(optsJSON))
	if err != nil {
		return failure(errOut, err)
	}
	result, err := wrapper.InstallRelease(ctx, cfgObj, chartObj, optionalGoString(chartRef), C.GoString(name), optionalGoString(valuesJSON), opts)
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.CString(result)
	return C.int32_t(cerrors.CodeOK)
}

// helm_upgrade upgrades release `name`. Chart selection and parameters as in
// helm_install.
//
//export helm_upgrade
func helm_upgrade(cfgH, ctxH, chartH C.uint64_t, chartRef *C.char, name *C.char, valuesJSON *C.char, optsJSON *C.char, out **C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if name == nil || out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "name and out must not be NULL"))
	}
	cfgObj, chartObj, err := configAndOptionalChart(cfgH, chartH)
	if err != nil {
		return failure(errOut, err)
	}
	ctx, err := ctxOrBackground(ctxH)
	if err != nil {
		return failure(errOut, err)
	}
	opts, err := wrapper.ParseUpgradeOptions(optionalGoString(optsJSON))
	if err != nil {
		return failure(errOut, err)
	}
	result, err := wrapper.UpgradeRelease(ctx, cfgObj, chartObj, optionalGoString(chartRef), C.GoString(name), optionalGoString(valuesJSON), opts)
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.CString(result)
	return C.int32_t(cerrors.CodeOK)
}

// releaseAction is the shared shim body for config+name → JSON actions.
func releaseAction(cfgH C.uint64_t, name *C.char, optsJSON *C.char, out **C.char, errOut **C.char,
	run func(cfgObj any, name, optsJSON string) (string, error)) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if name == nil || out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "name and out must not be NULL"))
	}
	cfgObj, err := registry.Get(uint64(cfgH), handles.TypeConfig)
	if err != nil {
		return failure(errOut, err)
	}
	result, err := run(cfgObj, C.GoString(name), optionalGoString(optsJSON))
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.CString(result)
	return C.int32_t(cerrors.CodeOK)
}

// helm_uninstall removes release `name`. opts_json optional. *out receives
// {"info":"...","release":{...}} JSON.
//
//export helm_uninstall
func helm_uninstall(cfgH C.uint64_t, name *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return releaseAction(cfgH, name, optsJSON, out, errOut, func(cfgObj any, name, optsJSON string) (string, error) {
		opts, err := wrapper.ParseUninstallOptions(optsJSON)
		if err != nil {
			return "", err
		}
		return wrapper.UninstallRelease(cfgObj, name, opts)
	})
}

// helm_rollback rolls release `name` back (opts key "version"; 0 = previous
// revision). No result payload — status code only.
//
//export helm_rollback
func helm_rollback(cfgH C.uint64_t, name *C.char, optsJSON *C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if name == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "name must not be NULL"))
	}
	cfgObj, err := registry.Get(uint64(cfgH), handles.TypeConfig)
	if err != nil {
		return failure(errOut, err)
	}
	opts, err := wrapper.ParseRollbackOptions(optionalGoString(optsJSON))
	if err != nil {
		return failure(errOut, err)
	}
	if err := wrapper.RollbackRelease(cfgObj, C.GoString(name), opts); err != nil {
		return failure(errOut, err)
	}
	return C.int32_t(cerrors.CodeOK)
}

// helm_list returns a JSON array of release summaries (no manifests).
// opts_json optional.
//
//export helm_list
func helm_list(cfgH C.uint64_t, optsJSON *C.char, out **C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "out must not be NULL"))
	}
	cfgObj, err := registry.Get(uint64(cfgH), handles.TypeConfig)
	if err != nil {
		return failure(errOut, err)
	}
	opts, err := wrapper.ParseListOptions(optionalGoString(optsJSON))
	if err != nil {
		return failure(errOut, err)
	}
	result, err := wrapper.ListReleases(cfgObj, opts)
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.CString(result)
	return C.int32_t(cerrors.CodeOK)
}

// helm_status returns the release summary JSON (with manifest) for `name`.
//
//export helm_status
func helm_status(cfgH C.uint64_t, name *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return releaseAction(cfgH, name, optsJSON, out, errOut, func(cfgObj any, name, optsJSON string) (string, error) {
		opts, err := wrapper.ParseStatusOptions(optsJSON)
		if err != nil {
			return "", err
		}
		return wrapper.StatusRelease(cfgObj, name, opts)
	})
}

// helm_history returns a JSON array of revision summaries for `name`.
//
//export helm_history
func helm_history(cfgH C.uint64_t, name *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return releaseAction(cfgH, name, optsJSON, out, errOut, func(cfgObj any, name, optsJSON string) (string, error) {
		opts, err := wrapper.ParseHistoryOptions(optsJSON)
		if err != nil {
			return "", err
		}
		return wrapper.HistoryRelease(cfgObj, name, opts)
	})
}

// helm_get_values returns the release's values as JSON (opts key "all" for
// computed values).
//
//export helm_get_values
func helm_get_values(cfgH C.uint64_t, name *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return releaseAction(cfgH, name, optsJSON, out, errOut, func(cfgObj any, name, optsJSON string) (string, error) {
		opts, err := wrapper.ParseGetValuesOptions(optsJSON)
		if err != nil {
			return "", err
		}
		return wrapper.GetReleaseValues(cfgObj, name, opts)
	})
}
