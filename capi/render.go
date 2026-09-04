package main

/*
#include <stdint.h>
*/
import "C"

import (
	"github.com/shivamkumar99/helm-c-sdk/internal/handles"
	"github.com/shivamkumar99/helm-c-sdk/pkg/wrapper"
)

// helm_chart_merge_values coalesces chart defaults with overrides
// (values_json optional, may be NULL; overrides win) and writes the effective
// values JSON into *out (caller frees with helm_free_string).
//
//export helm_chart_merge_values
func helm_chart_merge_values(h C.uint64_t, valuesJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return chartHandleShim(h, out, errOut, func(obj any) (string, error) {
		return wrapper.MergeChartValues(obj, optionalGoString(valuesJSON))
	})
}

// helm_schema_validate checks the effective values (chart defaults + optional
// overrides) against the chart's values.schema.json. A chart without a schema
// always passes.
//
//export helm_schema_validate
func helm_schema_validate(h C.uint64_t, valuesJSON *C.char, errOut **C.char) C.int32_t {
	return statusResult(errOut, func() error {
		obj, err := registry.Get(uint64(h), handles.TypeChart)
		if err != nil {
			return err
		}
		return wrapper.ValidateValuesSchema(obj, optionalGoString(valuesJSON))
	})
}

// helm_render renders the chart's templates offline (no cluster) and writes
// {"template path": "manifest", ...} JSON into *out (caller frees with
// helm_free_string). values_json and opts_json are optional (may be NULL).
//
//export helm_render
func helm_render(h C.uint64_t, valuesJSON *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return chartHandleShim(h, out, errOut, func(obj any) (string, error) {
		opts, err := wrapper.ParseRenderOptions(optionalGoString(optsJSON))
		if err != nil {
			return "", err
		}
		return wrapper.RenderChart(obj, optionalGoString(valuesJSON), opts)
	})
}
