package main

/*
#include <stdint.h>
#include <stdlib.h>
*/
import "C"

import (
	"math"
	"unsafe"

	"github.com/shivamkumar99/helm-c-sdk/internal/handles"
	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
	"github.com/shivamkumar99/helm-c-sdk/pkg/wrapper"
)

// ---------------------------------------------------------------------------
// Shared shim bodies. Every shim in this library has one of three shapes —
// "inputs → one string out", "inputs → status only", "handle in → free" —
// and each shape has exactly one implementation here.
// ---------------------------------------------------------------------------

// requireArgs is the NULL check every shim performs on its required C
// string parameters; names is the message noun list ("path and out").
func requireArgs(names string, ptrs ...*C.char) error {
	for _, p := range ptrs {
		if p == nil {
			return cerrors.New(cerrors.CodeInvalidArg, names+" must not be NULL")
		}
	}
	return nil
}

// stringResult is the shared body of every shim that yields one caller-owned
// string: error-out reset, panic guard, out-param check, delegate, convert.
func stringResult(out, errOut **C.char, run func() (string, error)) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "out must not be NULL"))
	}
	result, err := run()
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.CString(result)
	return C.int32_t(cerrors.CodeOK)
}

// handleResult is the shared body of every shim that yields one opaque
// handle: error-out reset, panic guard, out-param check, delegate, store.
func handleResult(out *C.uint64_t, errOut **C.char, run func() (uint64, error)) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "out must not be NULL"))
	}
	h, err := run()
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.uint64_t(h)
	return C.int32_t(cerrors.CodeOK)
}

// chartRefClientResult is the shared prologue of shims acting on a chart
// reference through an optional registry client (0 = default client).
func chartRefClientResult(
	clientH C.uint64_t,
	chartRef *C.char,
	out, errOut **C.char,
	run func(clientObj any, ref string) (string, error),
) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("chart_ref", chartRef); err != nil {
			return "", err
		}
		clientObj, err := registryClientOrNil(clientH)
		if err != nil {
			return "", err
		}
		return run(clientObj, C.GoString(chartRef))
	})
}

// statusResult is the shared body of every status-only shim.
func statusResult(errOut **C.char, run func() error) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if err := run(); err != nil {
		return failure(errOut, err)
	}
	return C.int32_t(cerrors.CodeOK)
}

// chartHandleShim resolves a chart handle and yields a string.
func chartHandleShim(h C.uint64_t, out, errOut **C.char, run func(obj any) (string, error)) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		obj, err := registry.Get(uint64(h), handles.TypeChart)
		if err != nil {
			return "", err
		}
		return run(obj)
	})
}

// freeTyped is the shared body of the per-type free shims: type-checked,
// idempotent, with an optional hook run on the object before release.
func freeTyped(h C.uint64_t, typ handles.Type, errOut **C.char, before func(obj any)) C.int32_t {
	return statusResult(errOut, func() error {
		if before != nil {
			obj, err := registry.Get(uint64(h), typ)
			if err != nil {
				return err
			}
			before(obj)
		}
		return registry.FreeTyped(uint64(h), typ)
	})
}

// ---------------------------------------------------------------------------
// Charts: content access, alternative loaders and writers
// ---------------------------------------------------------------------------

// helm_chart_files writes the chart's non-template files (README, LICENSE,
// …) as [{"name","data"}] JSON into *out (caller frees).
//
//export helm_chart_files
func helm_chart_files(h C.uint64_t, out **C.char, errOut **C.char) C.int32_t {
	return chartHandleShim(h, out, errOut, wrapper.ChartFilesJSON)
}

// helm_chart_templates writes the chart's raw templates as [{"name","data"}]
// JSON into *out (caller frees).
//
//export helm_chart_templates
func helm_chart_templates(h C.uint64_t, out **C.char, errOut **C.char) C.int32_t {
	return chartHandleShim(h, out, errOut, wrapper.ChartTemplatesJSON)
}

// helm_chart_crds writes the chart's (and subcharts') crds/ objects as
// [{"name","filename","data"}] JSON into *out (caller frees).
//
//export helm_chart_crds
func helm_chart_crds(h C.uint64_t, out **C.char, errOut **C.char) C.int32_t {
	return chartHandleShim(h, out, errOut, wrapper.ChartCRDsJSON)
}

// helm_chart_schema writes the chart's values.schema.json into *out, or
// "null" when the chart has none (caller frees).
//
//export helm_chart_schema
func helm_chart_schema(h C.uint64_t, out **C.char, errOut **C.char) C.int32_t {
	return chartHandleShim(h, out, errOut, wrapper.ChartSchemaJSON)
}

// helm_chart_dependencies writes the metadata of the subcharts loaded with
// the chart as a JSON array into *out (caller frees).
//
//export helm_chart_dependencies
func helm_chart_dependencies(h C.uint64_t, out **C.char, errOut **C.char) C.int32_t {
	return chartHandleShim(h, out, errOut, wrapper.ChartDependenciesJSON)
}

// archiveLength narrows the caller's buffer length to what C.GoBytes takes,
// refusing 0 and anything past the C int range so the narrowing below can
// never wrap.
func archiveLength(length uint64) (int32, bool) {
	if length == 0 || length > math.MaxInt32 {
		return 0, false
	}
	return int32(length), true // #nosec G115 -- bounded to MaxInt32 by the check above
}

// helm_chart_load_archive loads a chart from an in-memory .tgz buffer and
// returns a chart handle in *out (free with helm_chart_free). data is
// borrowed for the call only.
//
//export helm_chart_load_archive
func helm_chart_load_archive(data *C.uint8_t, length C.uint64_t, out *C.uint64_t, errOut **C.char) C.int32_t {
	return handleResult(out, errOut, func() (uint64, error) {
		if data == nil {
			return 0, cerrors.New(cerrors.CodeInvalidArg, "data must not be NULL")
		}
		n, ok := archiveLength(uint64(length))
		if !ok {
			return 0, cerrors.New(cerrors.CodeInvalidArg, "length must be 1..2^31-1")
		}
		// unsafe justification: C.GoBytes copies a caller-owned buffer of the
		// stated length into Go memory; the pointer is never retained (see
		// docs/DESIGN.md, "Use of unsafe").
		buf := C.GoBytes(unsafe.Pointer(data), C.int(n)) // #nosec G103 -- required by the C.GoBytes signature
		c, err := wrapper.LoadChartArchive(buf)
		if err != nil {
			return 0, err
		}
		return registry.Put(handles.TypeChart, c), nil
	})
}

// helm_chart_expand unpacks a local .tgz chart archive into dest_dir.
//
//export helm_chart_expand
func helm_chart_expand(destDir *C.char, archivePath *C.char, errOut **C.char) C.int32_t {
	return statusResult(errOut, func() error {
		if err := requireArgs("dest_dir and archive_path", destDir, archivePath); err != nil {
			return err
		}
		return wrapper.ExpandChartArchive(C.GoString(destDir), C.GoString(archivePath))
	})
}

// helm_chart_save_dir writes the chart back as a directory tree under
// dest_dir; *out_path receives the created chart directory (caller frees).
//
//export helm_chart_save_dir
func helm_chart_save_dir(h C.uint64_t, destDir *C.char, outPath **C.char, errOut **C.char) C.int32_t {
	return chartHandleShim(h, outPath, errOut, func(obj any) (string, error) {
		if err := requireArgs("dest_dir", destDir); err != nil {
			return "", err
		}
		return wrapper.SaveChartDir(obj, C.GoString(destDir))
	})
}

// helm_chart_create_from scaffolds a chart named name inside dir from the
// starter chart at starter_dir; *out_path receives the created directory.
//
//export helm_chart_create_from
func helm_chart_create_from(name *C.char, dir *C.char, starterDir *C.char, outPath **C.char, errOut **C.char) C.int32_t {
	return stringResult(outPath, errOut, func() (string, error) {
		if err := requireArgs("name, dir and starter_dir", name, dir, starterDir); err != nil {
			return "", err
		}
		return wrapper.CreateChartFrom(C.GoString(name), C.GoString(dir), C.GoString(starterDir))
	})
}

// helm_chart_digest writes the "sha256:..." digest of a chart archive into
// *out (caller frees).
//
//export helm_chart_digest
func helm_chart_digest(archivePath *C.char, out **C.char, errOut **C.char) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("archive_path", archivePath); err != nil {
			return "", err
		}
		return wrapper.ChartDigest(C.GoString(archivePath))
	})
}

// helm_chart_sign clear-signs a packaged chart archive (opts_json keys:
// key, keyring, passphrase_file); *out_prov_path receives the written
// .prov path (caller frees).
//
//export helm_chart_sign
func helm_chart_sign(archivePath *C.char, optsJSON *C.char, outProvPath **C.char, errOut **C.char) C.int32_t {
	return stringResult(outProvPath, errOut, func() (string, error) {
		if err := requireArgs("archive_path", archivePath); err != nil {
			return "", err
		}
		opts, err := wrapper.ParseSignOptions(optionalGoString(optsJSON))
		if err != nil {
			return "", err
		}
		return wrapper.SignChart(C.GoString(archivePath), opts)
	})
}

// helm_values_from_yaml parses a YAML values document into the JSON object
// every other function accepts; *out receives it (caller frees).
//
//export helm_values_from_yaml
func helm_values_from_yaml(yaml *C.char, out **C.char, errOut **C.char) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("yaml", yaml); err != nil {
			return "", err
		}
		return wrapper.ValuesFromYAML(C.GoString(yaml))
	})
}

// helm_show is `helm show`: the chart definition, values, README or CRDs of
// a chart reference (local path, repo chart via opts chart_repo_url, or
// oci://) without installing. client optional (0 = default). opts_json
// keys: format ("all"|"chart"|"values"|"readme"|"crds"), devel, plus the
// chart_ref resolution keys of helm_install. *out receives the SDK's text
// rendering (YAML/Markdown), caller frees.
//
//export helm_show
func helm_show(clientH C.uint64_t, chartRef *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return chartRefClientResult(clientH, chartRef, out, errOut, func(clientObj any, ref string) (string, error) {
		opts, err := wrapper.ParseShowOptions(optionalGoString(optsJSON))
		if err != nil {
			return "", err
		}
		return wrapper.ShowChart(clientObj, ref, opts)
	})
}

// helm_lint_run_opts is helm_lint_run with the full `helm lint` option set
// in opts_json: strict, namespace, with_subcharts, quiet,
// skip_schema_validation, kube_version.
//
//export helm_lint_run_opts
func helm_lint_run_opts(path *C.char, valuesJSON *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("path", path); err != nil {
			return "", err
		}
		opts, err := wrapper.ParseLintOptions(optionalGoString(optsJSON))
		if err != nil {
			return "", err
		}
		return wrapper.LintChartWithOptions(C.GoString(path), optionalGoString(valuesJSON), opts)
	})
}

// helm_render_with_config renders like helm_render but against the cluster
// behind config, so the `lookup` template function sees live objects.
// Nothing is created or stored. Blocking: cluster I/O.
//
//export helm_render_with_config
func helm_render_with_config(cfgH C.uint64_t, chartH C.uint64_t, valuesJSON *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		cfgObj, chartObj, err := configAndOptionalChart(cfgH, chartH)
		if err != nil {
			return "", err
		}
		if chartObj == nil {
			return "", cerrors.New(cerrors.CodeInvalidArg, "chart handle must not be 0")
		}
		opts, err := wrapper.ParseRenderOptions(optionalGoString(optsJSON))
		if err != nil {
			return "", err
		}
		return wrapper.RenderChartWithConfig(cfgObj, chartObj, optionalGoString(valuesJSON), opts)
	})
}

// ---------------------------------------------------------------------------
// --set family
// ---------------------------------------------------------------------------

// strvalsShim is the shared body of the four --set-style parsers.
func strvalsShim(s *C.char, out, errOut **C.char, parse func(string) (string, error)) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("s", s); err != nil {
			return "", err
		}
		return parse(C.GoString(s))
	})
}

// helm_strvals_parse_string is --set-string: values stay strings.
//
//export helm_strvals_parse_string
func helm_strvals_parse_string(s *C.char, out **C.char, errOut **C.char) C.int32_t {
	return strvalsShim(s, out, errOut, wrapper.ParseSetStringValues)
}

// helm_strvals_parse_json is --set-json: values are JSON documents.
//
//export helm_strvals_parse_json
func helm_strvals_parse_json(s *C.char, out **C.char, errOut **C.char) C.int32_t {
	return strvalsShim(s, out, errOut, wrapper.ParseSetJSON)
}

// helm_strvals_parse_literal is --set-literal: the value is taken verbatim.
//
//export helm_strvals_parse_literal
func helm_strvals_parse_literal(s *C.char, out **C.char, errOut **C.char) C.int32_t {
	return strvalsShim(s, out, errOut, wrapper.ParseSetLiteral)
}

// helm_strvals_parse_file is --set-file: each value names a file whose
// contents become the value.
//
//export helm_strvals_parse_file
func helm_strvals_parse_file(s *C.char, out **C.char, errOut **C.char) C.int32_t {
	return strvalsShim(s, out, errOut, wrapper.ParseSetFile)
}

// ---------------------------------------------------------------------------
// Repositories, dependencies, registries
// ---------------------------------------------------------------------------

// helm_repo_index_generate is `helm repo index`: indexes the *.tgz in dir
// into dir/index.yaml. opts_json keys: base_url, merge, json. *out receives
// the generated index as JSON (caller frees).
//
//export helm_repo_index_generate
func helm_repo_index_generate(dir *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("dir", dir); err != nil {
			return "", err
		}
		opts, err := wrapper.ParseRepoIndexGenerateOptions(optionalGoString(optsJSON))
		if err != nil {
			return "", err
		}
		return wrapper.GenerateRepoIndex(C.GoString(dir), opts)
	})
}

// helm_dependency_list is `helm dependency list`: each declared dependency
// with its status, as a JSON array in *out (caller frees).
//
//export helm_dependency_list
func helm_dependency_list(chartDir *C.char, out **C.char, errOut **C.char) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("chart_dir", chartDir); err != nil {
			return "", err
		}
		return wrapper.DependencyList(C.GoString(chartDir))
	})
}

// registryRefShim resolves a required registry-client handle plus a
// reference and yields a string.
func registryRefShim(clientH C.uint64_t, ref *C.char, out, errOut **C.char, run func(clientObj any, ref string) (string, error)) C.int32_t {
	return stringResult(out, errOut, func() (string, error) {
		if err := requireArgs("ref", ref); err != nil {
			return "", err
		}
		clientObj, err := registry.Get(uint64(clientH), handles.TypeRegistryClient)
		if err != nil {
			return "", err
		}
		return run(clientObj, C.GoString(ref))
	})
}

// helm_registry_tags lists the semver tags of an oci:// chart reference as a
// JSON array in *out (caller frees). Blocking: network I/O.
//
//export helm_registry_tags
func helm_registry_tags(clientH C.uint64_t, ref *C.char, out **C.char, errOut **C.char) C.int32_t {
	return registryRefShim(clientH, ref, out, errOut, wrapper.RegistryTags)
}

// helm_registry_resolve resolves an oci:// reference to its descriptor
// ({"digest","media_type","size"}) in *out (caller frees). Blocking:
// network I/O.
//
//export helm_registry_resolve
func helm_registry_resolve(clientH C.uint64_t, ref *C.char, out **C.char, errOut **C.char) C.int32_t {
	return registryRefShim(clientH, ref, out, errOut, wrapper.RegistryResolve)
}

// ---------------------------------------------------------------------------
// Cluster configuration extras and release actions
// ---------------------------------------------------------------------------

// helm_config_set_registry_client binds a registry client to the config so
// install/upgrade/show by an oci:// chart_ref use its credentials. client 0
// unbinds. The client handle must outlive the binding.
//
//export helm_config_set_registry_client
func helm_config_set_registry_client(cfgH C.uint64_t, clientH C.uint64_t, errOut **C.char) C.int32_t {
	return statusResult(errOut, func() error {
		cfgObj, err := registry.Get(uint64(cfgH), handles.TypeConfig)
		if err != nil {
			return err
		}
		clientObj, err := registryClientOrNil(clientH)
		if err != nil {
			return err
		}
		return wrapper.SetConfigRegistryClient(cfgObj, clientObj)
	})
}

// helm_config_check_reachable probes the config's cluster: HELM_OK when the
// API server answers, HELM_ERR_KUBE otherwise. Blocking: one round trip.
//
//export helm_config_check_reachable
func helm_config_check_reachable(cfgH C.uint64_t, errOut **C.char) C.int32_t {
	return statusResult(errOut, func() error {
		cfgObj, err := registry.Get(uint64(cfgH), handles.TypeConfig)
		if err != nil {
			return err
		}
		return wrapper.CheckReachable(cfgObj)
	})
}

// helm_get is `helm get all`: the full stored release —
// {"summary":{...},"hooks":[...],"config":{...},"info":{...}} — for
// `name`. opts_json keys: revision (0 = latest).
//
//export helm_get
func helm_get(cfgH C.uint64_t, name *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return releaseAction(releaseShimArgs{cfgH, name, optsJSON, out, errOut}, func(cfgObj any, name, optsJSON string) (string, error) {
		opts, err := wrapper.ParseGetOptions(optsJSON)
		if err != nil {
			return "", err
		}
		return wrapper.GetRelease(cfgObj, name, opts)
	})
}

// helm_test_run is `helm test`: runs the release's test hooks. opts_json
// keys: timeout_seconds, logs, include_names, exclude_names. *out receives
// {"release":{summary},"logs":"..."}. A failing test is HELM_ERR_RELEASE.
// Blocking: cluster I/O, up to the timeout.
//
//export helm_test_run
func helm_test_run(cfgH C.uint64_t, name *C.char, optsJSON *C.char, out **C.char, errOut **C.char) C.int32_t {
	return releaseAction(releaseShimArgs{cfgH, name, optsJSON, out, errOut}, func(cfgObj any, name, optsJSON string) (string, error) {
		opts, err := wrapper.ParseTestOptions(optsJSON)
		if err != nil {
			return "", err
		}
		return wrapper.TestRelease(cfgObj, name, opts)
	})
}
