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

// helm_release_name_validate checks name against Helm's release-name rules.
// Returns HELM_OK for a valid name, HELM_ERR_INVALID_ARG otherwise (detail in
// *error_out, freed by the caller with helm_free_string).
//
//export helm_release_name_validate
func helm_release_name_validate(name *C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if name == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "name must not be NULL"))
	}
	if err := wrapper.ValidateReleaseName(C.GoString(name)); err != nil {
		return failure(errOut, err)
	}
	return C.int32_t(cerrors.CodeOK)
}

// helm_chart_load loads a chart from a directory or .tgz archive and returns
// an opaque chart handle in *out. Free with helm_chart_free.
//
//export helm_chart_load
func helm_chart_load(path *C.char, out *C.uint64_t, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if path == nil || out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "path and out must not be NULL"))
	}
	c, err := wrapper.LoadChart(C.GoString(path))
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.uint64_t(registry.Put(handles.TypeChart, c))
	return C.int32_t(cerrors.CodeOK)
}

// helm_chart_metadata writes the chart's Chart.yaml metadata as JSON into
// *out (caller frees with helm_free_string).
//
//export helm_chart_metadata
func helm_chart_metadata(h C.uint64_t, out **C.char, errOut **C.char) (code C.int32_t) {
	return chartToJSON(h, out, errOut, wrapper.ChartMetadataJSON)
}

// helm_chart_values writes the chart's default values as JSON into *out
// (caller frees with helm_free_string).
//
//export helm_chart_values
func helm_chart_values(h C.uint64_t, out **C.char, errOut **C.char) (code C.int32_t) {
	return chartToJSON(h, out, errOut, wrapper.ChartValuesJSON)
}

// chartToJSON is the shared shim body for chart→JSON accessors.
func chartToJSON(h C.uint64_t, out **C.char, errOut **C.char, render func(any) (string, error)) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "out must not be NULL"))
	}
	obj, err := registry.Get(uint64(h), handles.TypeChart)
	if err != nil {
		return failure(errOut, err)
	}
	jsonStr, err := render(obj)
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.CString(jsonStr)
	return C.int32_t(cerrors.CodeOK)
}

// helm_chart_save archives the chart into dest_dir; *out_path receives the
// .tgz path (caller frees with helm_free_string).
//
//export helm_chart_save
func helm_chart_save(h C.uint64_t, destDir *C.char, outPath **C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if destDir == nil || outPath == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "dest_dir and out_path must not be NULL"))
	}
	obj, err := registry.Get(uint64(h), handles.TypeChart)
	if err != nil {
		return failure(errOut, err)
	}
	path, err := wrapper.SaveChart(obj, C.GoString(destDir))
	if err != nil {
		return failure(errOut, err)
	}
	*outPath = C.CString(path)
	return C.int32_t(cerrors.CodeOK)
}

// helm_chart_create scaffolds a new chart named name inside dir; *out_path
// receives the created chart directory (caller frees with helm_free_string).
//
//export helm_chart_create
func helm_chart_create(name *C.char, dir *C.char, outPath **C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if name == nil || dir == nil || outPath == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "name, dir and out_path must not be NULL"))
	}
	path, err := wrapper.CreateChart(C.GoString(name), C.GoString(dir))
	if err != nil {
		return failure(errOut, err)
	}
	*outPath = C.CString(path)
	return C.int32_t(cerrors.CodeOK)
}

// helm_chart_free releases a chart handle. Type-checked: freeing a non-chart
// handle returns HELM_ERR_WRONG_HANDLE_TYPE and leaves it alive.
//
//export helm_chart_free
func helm_chart_free(h C.uint64_t, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if err := registry.FreeTyped(uint64(h), handles.TypeChart); err != nil {
		return failure(errOut, err)
	}
	return C.int32_t(cerrors.CodeOK)
}

// helm_lint_run lints the chart at path (values_json optional, may be NULL)
// and writes a JSON report into *out. Lint findings are data in the report —
// the call itself only fails on malformed input.
//
//export helm_lint_run
func helm_lint_run(path *C.char, valuesJSON *C.char, out **C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if path == nil || out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "path and out must not be NULL"))
	}
	report, err := wrapper.LintChart(C.GoString(path), optionalGoString(valuesJSON))
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.CString(report)
	return C.int32_t(cerrors.CodeOK)
}

// helm_package_run packages the chart at path into a .tgz (opts_json
// optional, may be NULL — see docs/API.md for keys); *out_path receives the
// archive path (caller frees with helm_free_string).
//
//export helm_package_run
func helm_package_run(path *C.char, optsJSON *C.char, outPath **C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if path == nil || outPath == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "path and out_path must not be NULL"))
	}
	opts, err := wrapper.ParsePackageOptions(optionalGoString(optsJSON))
	if err != nil {
		return failure(errOut, err)
	}
	out, err := wrapper.PackageChart(C.GoString(path), opts)
	if err != nil {
		return failure(errOut, err)
	}
	*outPath = C.CString(out)
	return C.int32_t(cerrors.CodeOK)
}
