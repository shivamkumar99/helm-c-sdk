package main

/*
#include <stdint.h>
*/
import "C"

import (
	"github.com/shivamkumar99/helm-c-sdk/internal/handles"
	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
	"github.com/shivamkumar99/helm-c-sdk/pkg/wrapper"
)

// helm_chart_merge_values coalesces chart defaults with overrides
// (values_json optional, may be NULL; overrides win) and writes the effective
// values JSON into *out (caller frees with helm_free_string).
//
//export helm_chart_merge_values
func helm_chart_merge_values(h C.uint64_t, valuesJSON *C.char, out **C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "out must not be NULL"))
	}
	obj, err := registry.Get(uint64(h), handles.TypeChart)
	if err != nil {
		return failure(errOut, err)
	}
	merged, err := wrapper.MergeChartValues(obj, optionalGoString(valuesJSON))
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.CString(merged)
	return C.int32_t(cerrors.CodeOK)
}

// helm_schema_validate checks the effective values (chart defaults + optional
// overrides) against the chart's values.schema.json. A chart without a schema
// always passes.
//
//export helm_schema_validate
func helm_schema_validate(h C.uint64_t, valuesJSON *C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	obj, err := registry.Get(uint64(h), handles.TypeChart)
	if err != nil {
		return failure(errOut, err)
	}
	if err := wrapper.ValidateValuesSchema(obj, optionalGoString(valuesJSON)); err != nil {
		return failure(errOut, err)
	}
	return C.int32_t(cerrors.CodeOK)
}

// helm_render renders the chart's templates offline (no cluster) and writes
// {"template path": "manifest", ...} JSON into *out (caller frees with
// helm_free_string). values_json and opts_json are optional (may be NULL).
//
//export helm_render
func helm_render(h C.uint64_t, valuesJSON *C.char, optsJSON *C.char, out **C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "out must not be NULL"))
	}
	obj, err := registry.Get(uint64(h), handles.TypeChart)
	if err != nil {
		return failure(errOut, err)
	}
	opts, err := wrapper.ParseRenderOptions(optionalGoString(optsJSON))
	if err != nil {
		return failure(errOut, err)
	}
	manifests, err := wrapper.RenderChart(obj, optionalGoString(valuesJSON), opts)
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.CString(manifests)
	return C.int32_t(cerrors.CodeOK)
}
