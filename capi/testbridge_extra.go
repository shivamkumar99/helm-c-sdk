package main

/*
#include <stdint.h>
#include <stdlib.h>
*/
import "C"

import "unsafe"

// Test seam for the symbols added after 0.1.0 (see testbridge.go for why
// this lives outside _test.go).

// chartOut bridges the "chart handle → string" shims.
func chartOut(h uint64, fn func(C.uint64_t, **C.char, **C.char) C.int32_t) (int32, string, string) {
	var out, errOut *C.char
	code := fn(C.uint64_t(h), &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testChartFiles(h uint64) (int32, string, string)     { return chartOut(h, helm_chart_files) }
func testChartTemplates(h uint64) (int32, string, string) { return chartOut(h, helm_chart_templates) }
func testChartCRDs(h uint64) (int32, string, string)      { return chartOut(h, helm_chart_crds) }
func testChartSchema(h uint64) (int32, string, string)    { return chartOut(h, helm_chart_schema) }
func testChartDependencies(h uint64) (int32, string, string) {
	return chartOut(h, helm_chart_dependencies)
}

// strOut bridges the "one C string → string" shims.
func strOut(s *string, fn func(*C.char, **C.char, **C.char) C.int32_t) (int32, string, string) {
	cs, cleanup := optionalCString(s)
	defer cleanup()
	var out, errOut *C.char
	code := fn(cs, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testChartDigest(path *string) (int32, string, string) { return strOut(path, helm_chart_digest) }
func testValuesFromYAML(y *string) (int32, string, string) { return strOut(y, helm_values_from_yaml) }
func testDependencyList(dir *string) (int32, string, string) {
	return strOut(dir, helm_dependency_list)
}
func testStrvalsString(s *string) (int32, string, string) {
	return strOut(s, helm_strvals_parse_string)
}
func testStrvalsJSON(s *string) (int32, string, string) { return strOut(s, helm_strvals_parse_json) }
func testStrvalsLiteral(s *string) (int32, string, string) {
	return strOut(s, helm_strvals_parse_literal)
}
func testStrvalsFile(s *string) (int32, string, string) { return strOut(s, helm_strvals_parse_file) }

// strOptsOut bridges the "C string + opts_json → string" shims.
func strOptsOut(s *string, opts *string, fn func(*C.char, *C.char, **C.char, **C.char) C.int32_t) (int32, string, string) {
	cs, c1 := optionalCString(s)
	defer c1()
	cOpts, c2 := optionalCString(opts)
	defer c2()
	var out, errOut *C.char
	code := fn(cs, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testChartSign(path, opts *string) (int32, string, string) {
	return strOptsOut(path, opts, helm_chart_sign)
}
func testRepoIndexGenerate(dir, opts *string) (int32, string, string) {
	return strOptsOut(dir, opts, helm_repo_index_generate)
}

func testChartLoadArchive(data []byte) (int32, uint64, string) {
	var out C.uint64_t
	var errOut *C.char
	var ptr *C.uint8_t
	if len(data) > 0 {
		ptr = (*C.uint8_t)(unsafe.Pointer(&data[0])) // #nosec G103 -- test seam: hands a Go buffer to the shim for the call's duration
	}
	code := helm_chart_load_archive(ptr, C.uint64_t(len(data)), &out, &errOut)
	return int32(code), uint64(out), takeDetail(errOut)
}

func testChartExpand(dest, archive *string) (int32, string) {
	cDest, c1 := optionalCString(dest)
	defer c1()
	cArc, c2 := optionalCString(archive)
	defer c2()
	var errOut *C.char
	code := helm_chart_expand(cDest, cArc, &errOut)
	return int32(code), takeDetail(errOut)
}

func testChartSaveDir(h uint64, dest *string) (int32, string, string) {
	cDest, cleanup := optionalCString(dest)
	defer cleanup()
	var out, errOut *C.char
	code := helm_chart_save_dir(C.uint64_t(h), cDest, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testChartCreateFrom(name, dir, starter *string) (int32, string, string) {
	cName, c1 := optionalCString(name)
	defer c1()
	cDir, c2 := optionalCString(dir)
	defer c2()
	cStarter, c3 := optionalCString(starter)
	defer c3()
	var out, errOut *C.char
	code := helm_chart_create_from(cName, cDir, cStarter, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testShow(clientH uint64, ref, opts *string) (int32, string, string) {
	cRef, c1 := optionalCString(ref)
	defer c1()
	cOpts, c2 := optionalCString(opts)
	defer c2()
	var out, errOut *C.char
	code := helm_show(C.uint64_t(clientH), cRef, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testLintRunOpts(path string, vals, opts *string) (int32, string, string) {
	cPath := C.CString(path)
	defer freeCString(cPath)
	cVals, c1 := optionalCString(vals)
	defer c1()
	cOpts, c2 := optionalCString(opts)
	defer c2()
	var out, errOut *C.char
	code := helm_lint_run_opts(cPath, cVals, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testRenderWithConfig(cfgH, chartH uint64, vals, opts *string) (int32, string, string) {
	cVals, c1 := optionalCString(vals)
	defer c1()
	cOpts, c2 := optionalCString(opts)
	defer c2()
	var out, errOut *C.char
	code := helm_render_with_config(C.uint64_t(cfgH), C.uint64_t(chartH), cVals, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testRegistryRef(clientH uint64, ref *string, fn func(C.uint64_t, *C.char, **C.char, **C.char) C.int32_t) (int32, string, string) {
	cRef, cleanup := optionalCString(ref)
	defer cleanup()
	var out, errOut *C.char
	code := fn(C.uint64_t(clientH), cRef, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testRegistryTags(clientH uint64, ref *string) (int32, string, string) {
	return testRegistryRef(clientH, ref, helm_registry_tags)
}

func testRegistryResolve(clientH uint64, ref *string) (int32, string, string) {
	return testRegistryRef(clientH, ref, helm_registry_resolve)
}

func testConfigSetRegistryClient(cfgH, clientH uint64) (int32, string) {
	var errOut *C.char
	code := helm_config_set_registry_client(C.uint64_t(cfgH), C.uint64_t(clientH), &errOut)
	return int32(code), takeDetail(errOut)
}

func testConfigCheckReachable(cfgH uint64) (int32, string) {
	var errOut *C.char
	code := helm_config_check_reachable(C.uint64_t(cfgH), &errOut)
	return int32(code), takeDetail(errOut)
}

func testGet(cfgH uint64, name string, opts *string) (int32, string, string) {
	cName := C.CString(name)
	defer freeCString(cName)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var out, errOut *C.char
	code := helm_get(C.uint64_t(cfgH), cName, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testTestRun(cfgH uint64, name string, opts *string) (int32, string, string) {
	cName := C.CString(name)
	defer freeCString(cName)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var out, errOut *C.char
	code := helm_test_run(C.uint64_t(cfgH), cName, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}
