package main

/*
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "logbridge.h"

// Test-only capture callback (definitions are allowed here: this file has no
// //export declarations).
static int32_t helmc_test_log_count = 0;
static int32_t helmc_test_log_last_level = -1;
static char helmc_test_log_last_msg[512];

static void helmc_test_log_cb(int32_t level, const char* message, void* user_data) {
	(void)user_data;
	helmc_test_log_count++;
	helmc_test_log_last_level = level;
	strncpy(helmc_test_log_last_msg, message, sizeof(helmc_test_log_last_msg) - 1);
	helmc_test_log_last_msg[sizeof(helmc_test_log_last_msg) - 1] = '\0';
}

static helmc_log_callback helmc_test_log_cb_ptr(void) { return helmc_test_log_cb; }
static int32_t helmc_test_log_get_count(void) { return helmc_test_log_count; }
static int32_t helmc_test_log_get_level(void) { return helmc_test_log_last_level; }
static const char* helmc_test_log_get_msg(void) { return helmc_test_log_last_msg; }
static void helmc_test_log_reset(void) {
	helmc_test_log_count = 0;
	helmc_test_log_last_level = -1;
	helmc_test_log_last_msg[0] = '\0';
}
*/
import "C"

// This file exists because cgo is not permitted inside _test.go files: it
// exposes pure-Go entry points over the C shims so capi_test.go can exercise
// the real boundary code (conversion, panic guard, ownership) with C types.
// Everything here is unexported and test-only by convention.

func testCVersion() string {
	s := helm_c_version()
	defer freeCString(s)
	return C.GoString(s)
}

func testSDKVersion() string {
	s := helm_sdk_version()
	defer freeCString(s)
	return C.GoString(s)
}

func testFreeStringNil() { helm_free_string(nil) }

// takeDetail converts and frees an error out-param.
func takeDetail(errOut *C.char) string {
	if errOut == nil {
		return ""
	}
	defer freeCString(errOut)
	return C.GoString(errOut)
}

func testHandleFree(id uint64, wantDetail bool) (int32, string) {
	if !wantDetail {
		return int32(helm_handle_free(C.uint64_t(id), nil)), ""
	}
	var errOut *C.char
	code := helm_handle_free(C.uint64_t(id), &errOut)
	return int32(code), takeDetail(errOut)
}

// testReleaseNameValidate passes a NULL C string when name is nil.
func testReleaseNameValidate(name *string) (int32, string) {
	var cName *C.char
	if name != nil {
		cName = C.CString(*name)
		defer freeCString(cName)
	}
	var errOut *C.char
	code := helm_release_name_validate(cName, &errOut)
	return int32(code), takeDetail(errOut)
}

func testStrvalsParse(s string) (int32, string, string) {
	cs := C.CString(s)
	defer freeCString(cs)
	var out, errOut *C.char
	code := helm_strvals_parse(cs, &out, &errOut)
	return int32(code), takeDetail(out), takeDetail(errOut)
}

func testChartLoad(path string) (int32, uint64, string) {
	cs := C.CString(path)
	defer freeCString(cs)
	var out C.uint64_t
	var errOut *C.char
	code := helm_chart_load(cs, &out, &errOut)
	return int32(code), uint64(out), takeDetail(errOut)
}

func testChartMetadata(h uint64) (int32, string, string) {
	var out, errOut *C.char
	code := helm_chart_metadata(C.uint64_t(h), &out, &errOut)
	return int32(code), takeDetail(out), takeDetail(errOut)
}

func testChartFree(h uint64) (int32, string) {
	var errOut *C.char
	code := helm_chart_free(C.uint64_t(h), &errOut)
	return int32(code), takeDetail(errOut)
}

// optionalCString allocates a C string for non-nil input, NULL otherwise;
// caller must free via the returned cleanup.
func optionalCString(s *string) (*C.char, func()) {
	if s == nil {
		return nil, func() { /* nothing was allocated for a NULL argument */ }
	}
	cs := C.CString(*s)
	return cs, func() { freeCString(cs) }
}

func testMergeValues(h uint64, valuesJSON *string) (int32, string, string) {
	cVals, cleanup := optionalCString(valuesJSON)
	defer cleanup()
	var out, errOut *C.char
	code := helm_chart_merge_values(C.uint64_t(h), cVals, &out, &errOut)
	return int32(code), takeDetail(out), takeDetail(errOut)
}

func testSchemaValidate(h uint64, valuesJSON *string) (int32, string) {
	cVals, cleanup := optionalCString(valuesJSON)
	defer cleanup()
	var errOut *C.char
	code := helm_schema_validate(C.uint64_t(h), cVals, &errOut)
	return int32(code), takeDetail(errOut)
}

func testRender(h uint64, valuesJSON, optsJSON *string) (int32, string, string) {
	cVals, cleanupVals := optionalCString(valuesJSON)
	defer cleanupVals()
	cOpts, cleanupOpts := optionalCString(optsJSON)
	defer cleanupOpts()
	var out, errOut *C.char
	code := helm_render(C.uint64_t(h), cVals, cOpts, &out, &errOut)
	return int32(code), takeDetail(out), takeDetail(errOut)
}

func testRegistryClientNew(optsJSON *string) (int32, uint64, string) {
	cOpts, cleanup := optionalCString(optsJSON)
	defer cleanup()
	var out C.uint64_t
	var errOut *C.char
	code := helm_registry_client_new(cOpts, &out, &errOut)
	return int32(code), uint64(out), takeDetail(errOut)
}

func testRegistryClientFree(h uint64) (int32, string) {
	var errOut *C.char
	code := helm_registry_client_free(C.uint64_t(h), &errOut)
	return int32(code), takeDetail(errOut)
}

func testConfigNew(optsJSON *string) (int32, uint64, string) {
	cOpts, cleanup := optionalCString(optsJSON)
	defer cleanup()
	var out C.uint64_t
	var errOut *C.char
	code := helm_config_new(cOpts, &out, &errOut)
	return int32(code), uint64(out), takeDetail(errOut)
}

func testConfigFree(h uint64) (int32, string) {
	var errOut *C.char
	code := helm_config_free(C.uint64_t(h), &errOut)
	return int32(code), takeDetail(errOut)
}

func testList(cfgH uint64, optsJSON *string) (int32, string, string) {
	cOpts, cleanup := optionalCString(optsJSON)
	defer cleanup()
	var out, errOut *C.char
	code := helm_list(C.uint64_t(cfgH), cOpts, &out, &errOut)
	return int32(code), takeDetail(out), takeDetail(errOut)
}

func testContextLifecycle() (newCode, cancelCode, freeCode, doubleFreeCode int32) {
	var h C.uint64_t
	newCode = int32(helm_context_new(&h, nil))
	cancelCode = int32(helm_context_cancel(h, nil))
	freeCode = int32(helm_context_free(h, nil))
	doubleFreeCode = int32(helm_context_free(h, nil))
	return newCode, cancelCode, freeCode, doubleFreeCode
}

// testLogHandlerInstall registers the C capture callback at minLevel.
func testLogHandlerInstall(minLevel int32) int32 {
	return int32(helm_set_log_handler(C.helmc_test_log_cb_ptr(), nil, C.int32_t(minLevel)))
}

func testLogHandlerClear() int32 {
	return int32(helm_set_log_handler(nil, nil, 0))
}

func testLogCaptured() (count int32, level int32, msg string) {
	return int32(C.helmc_test_log_get_count()),
		int32(C.helmc_test_log_get_level()),
		C.GoString(C.helmc_test_log_get_msg())
}

func testLogReset() { C.helmc_test_log_reset() }

func testPanicGuard() (int32, string) {
	var errOut *C.char
	var code C.int32_t
	func() {
		defer recoverToCode(&code, &errOut)
		panic("deliberate test panic")
	}()
	return int32(code), takeDetail(errOut)
}

// --- bridges for the remaining shims (every //export gets boundary tests) ---

func bridgeOut2(code C.int32_t, out, errOut *C.char) (int32, string, string) {
	return int32(code), takeDetail(out), takeDetail(errOut)
}

func testChartValues(h uint64) (int32, string, string) {
	var out, errOut *C.char
	code := helm_chart_values(C.uint64_t(h), &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testChartSave(h uint64, dir string) (int32, string, string) {
	cDir := C.CString(dir)
	defer freeCString(cDir)
	var out, errOut *C.char
	code := helm_chart_save(C.uint64_t(h), cDir, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testChartCreate(name, dir string) (int32, string, string) {
	cName, cDir := C.CString(name), C.CString(dir)
	defer freeCString(cName)
	defer freeCString(cDir)
	var out, errOut *C.char
	code := helm_chart_create(cName, cDir, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testLintRun(path string, valuesJSON *string) (int32, string, string) {
	cPath := C.CString(path)
	defer freeCString(cPath)
	cVals, cleanup := optionalCString(valuesJSON)
	defer cleanup()
	var out, errOut *C.char
	code := helm_lint_run(cPath, cVals, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testPackageRun(path string, optsJSON *string) (int32, string, string) {
	cPath := C.CString(path)
	defer freeCString(cPath)
	cOpts, cleanup := optionalCString(optsJSON)
	defer cleanup()
	var out, errOut *C.char
	code := helm_package_run(cPath, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testChartVerify(path string, prov *string, keyring string) (int32, string, string) {
	cPath, cKeyring := C.CString(path), C.CString(keyring)
	defer freeCString(cPath)
	defer freeCString(cKeyring)
	cProv, cleanup := optionalCString(prov)
	defer cleanup()
	var out, errOut *C.char
	code := helm_chart_verify(cPath, cProv, cKeyring, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testOpenHandlesCount() int64 { return int64(helm_open_handles_count()) }

func testInstall(cfgH, ctxH, chartH uint64, chartRef *string, name string, vals, opts *string) (int32, string, string) {
	cRef, c1 := optionalCString(chartRef)
	defer c1()
	cName := C.CString(name)
	defer freeCString(cName)
	cVals, c2 := optionalCString(vals)
	defer c2()
	cOpts, c3 := optionalCString(opts)
	defer c3()
	var out, errOut *C.char
	code := helm_install(C.uint64_t(cfgH), C.uint64_t(ctxH), C.uint64_t(chartH), cRef, cName, cVals, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testUpgrade(cfgH, ctxH, chartH uint64, chartRef *string, name string, vals, opts *string) (int32, string, string) {
	cRef, c1 := optionalCString(chartRef)
	defer c1()
	cName := C.CString(name)
	defer freeCString(cName)
	cVals, c2 := optionalCString(vals)
	defer c2()
	cOpts, c3 := optionalCString(opts)
	defer c3()
	var out, errOut *C.char
	code := helm_upgrade(C.uint64_t(cfgH), C.uint64_t(ctxH), C.uint64_t(chartH), cRef, cName, cVals, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

// nameAction bridges the cfg+name → out shims.
func testUninstall(cfgH uint64, name string, opts *string) (int32, string, string) {
	cName := C.CString(name)
	defer freeCString(cName)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var out, errOut *C.char
	code := helm_uninstall(C.uint64_t(cfgH), cName, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testRollback(cfgH uint64, name string, opts *string) (int32, string) {
	cName := C.CString(name)
	defer freeCString(cName)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var errOut *C.char
	code := helm_rollback(C.uint64_t(cfgH), cName, cOpts, &errOut)
	return int32(code), takeDetail(errOut)
}

func testStatus(cfgH uint64, name string, opts *string) (int32, string, string) {
	cName := C.CString(name)
	defer freeCString(cName)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var out, errOut *C.char
	code := helm_status(C.uint64_t(cfgH), cName, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testHistory(cfgH uint64, name string, opts *string) (int32, string, string) {
	cName := C.CString(name)
	defer freeCString(cName)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var out, errOut *C.char
	code := helm_history(C.uint64_t(cfgH), cName, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testGetValues(cfgH uint64, name string, opts *string) (int32, string, string) {
	cName := C.CString(name)
	defer freeCString(cName)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var out, errOut *C.char
	code := helm_get_values(C.uint64_t(cfgH), cName, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testGetMetadata(cfgH uint64, name string, opts *string) (int32, string, string) {
	cName := C.CString(name)
	defer freeCString(cName)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var out, errOut *C.char
	code := helm_get_metadata(C.uint64_t(cfgH), cName, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testPull(clientH uint64, ref string, opts *string) (int32, string, string) {
	cRef := C.CString(ref)
	defer freeCString(cRef)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var out, errOut *C.char
	code := helm_pull(C.uint64_t(clientH), cRef, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testPush(clientH uint64, path, remote string, opts *string) (int32, string, string) {
	cPath, cRemote := C.CString(path), C.CString(remote)
	defer freeCString(cPath)
	defer freeCString(cRemote)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var out, errOut *C.char
	code := helm_push(C.uint64_t(clientH), cPath, cRemote, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testRegistryLogin(h uint64, host, user, pass string, opts *string) (int32, string) {
	cHost, cUser, cPass := C.CString(host), C.CString(user), C.CString(pass)
	defer freeCString(cHost)
	defer freeCString(cUser)
	defer freeCString(cPass)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var errOut *C.char
	code := helm_registry_login(C.uint64_t(h), cHost, cUser, cPass, cOpts, &errOut)
	return int32(code), takeDetail(errOut)
}

func testRegistryLogout(h uint64, host string) (int32, string) {
	cHost := C.CString(host)
	defer freeCString(cHost)
	var errOut *C.char
	code := helm_registry_logout(C.uint64_t(h), cHost, &errOut)
	return int32(code), takeDetail(errOut)
}

func testRepoIndexDownload(url string, opts *string) (int32, string, string) {
	cURL := C.CString(url)
	defer freeCString(cURL)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var out, errOut *C.char
	code := helm_repo_index_download(cURL, cOpts, &out, &errOut)
	return bridgeOut2(code, out, errOut)
}

func testDependencyUpdate(dir string, opts *string) (int32, string) {
	cDir := C.CString(dir)
	defer freeCString(cDir)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var errOut *C.char
	code := helm_dependency_update(cDir, cOpts, &errOut)
	return int32(code), takeDetail(errOut)
}

func testDependencyBuild(dir string, opts *string) (int32, string) {
	cDir := C.CString(dir)
	defer freeCString(cDir)
	cOpts, cleanup := optionalCString(opts)
	defer cleanup()
	var errOut *C.char
	code := helm_dependency_build(cDir, cOpts, &errOut)
	return int32(code), takeDetail(errOut)
}
