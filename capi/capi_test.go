package main

import (
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shivamkumar99/helm-c-sdk/internal/handles"
	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
	"github.com/shivamkumar99/helm-c-sdk/pkg/wrapper"
)

func TestVersions(t *testing.T) {
	assert.Equal(t, libVersion, testCVersion())

	sdk := testSDKVersion()
	assert.True(t, strings.HasPrefix(sdk, "v4."),
		"sdk version should be the pinned helm release, got %q", sdk)
}

func TestFreeStringNULLSafe(t *testing.T) {
	testFreeStringNil() // must not crash
}

func TestHandleFreeInvalid(t *testing.T) {
	code, detail := testHandleFree(0, true)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code)
	assert.NotEmpty(t, detail)

	// NULL error_out is a supported way to decline detail.
	code, _ = testHandleFree(12345, false)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code)
}

func TestHandleLifecycleThroughCAPI(t *testing.T) {
	require.EqualValues(t, 0, registry.Count())

	id := registry.Put(handles.TypeChart, "placeholder")
	assert.EqualValues(t, 1, registry.Count())

	code, detail := testHandleFree(id, true)
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Empty(t, detail)
	assert.EqualValues(t, 0, registry.Count())

	// Double-free: defined error, no crash.
	code, detail = testHandleFree(id, true)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code)
	assert.NotEmpty(t, detail)
}

func TestReleaseNameValidate(t *testing.T) {
	good := "good-name"
	code, detail := testReleaseNameValidate(&good)
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Empty(t, detail)

	bad := "Bad_Name!"
	code, detail = testReleaseNameValidate(&bad)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
	assert.NotEmpty(t, detail)

	code, detail = testReleaseNameValidate(nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
	assert.Contains(t, detail, "NULL")
}

func TestRecoverToCodeConvertsPanic(t *testing.T) {
	code, detail := testPanicGuard()
	assert.EqualValues(t, cerrors.CodePanic, code)
	assert.Contains(t, detail, "deliberate test panic")
}

func TestStrvalsParseThroughCAPI(t *testing.T) {
	code, out, detail := testStrvalsParse("a=1,b=two")
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.JSONEq(t, `{"a":1,"b":"two"}`, out)
	assert.Empty(t, detail)

	code, _, detail = testStrvalsParse("a=1,,=x=")
	assert.EqualValues(t, cerrors.CodeValues, code)
	assert.NotEmpty(t, detail)
}

func TestChartLifecycleThroughCAPI(t *testing.T) {
	require.EqualValues(t, 0, registry.Count())

	code, h, detail := testChartLoad(fixtureChart)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	require.NotZero(t, h)
	assert.EqualValues(t, 1, registry.Count())

	code, meta, detail := testChartMetadata(h)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, meta, `"name":"testchart"`)

	code, detail = testChartFree(h)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.EqualValues(t, 0, registry.Count())

	// Metadata on a freed handle: defined error.
	code, _, _ = testChartMetadata(h)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code)

	// Double-free: defined error.
	code, _ = testChartFree(h)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code)
}

func TestChartLoadFailureThroughCAPI(t *testing.T) {
	code, h, detail := testChartLoad(t.TempDir() + "/does-not-exist")
	assert.EqualValues(t, cerrors.CodeChartLoad, code)
	assert.Zero(t, h)
	assert.NotEmpty(t, detail)
}

func TestValuesAndRenderThroughCAPI(t *testing.T) {
	code, h, detail := testChartLoad(fixtureChart)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	defer func() {
		code, _ := testChartFree(h)
		require.EqualValues(t, cerrors.CodeOK, code)
	}()

	overrides := `{"replicaCount":7}`
	code, merged, detail := testMergeValues(h, &overrides)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, merged, `"replicaCount":7`)

	// NULL values_json: defaults only.
	code, merged, _ = testMergeValues(h, nil)
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Contains(t, merged, `"replicaCount":1`)

	code, detail = testSchemaValidate(h, nil)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)

	opts := `{"name":"boundary-rel"}`
	code, out, detail := testRender(h, &overrides, &opts)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, out, "boundary-rel-config")
	assert.Contains(t, out, `replicas: \"7\"`)

	badOpts := `{"nope":1}`
	code, _, detail = testRender(h, nil, &badOpts)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
	assert.NotEmpty(t, detail)
}

func TestSchemaValidateFailureThroughCAPI(t *testing.T) {
	code, h, detail := testChartLoad(fixtureSchema)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	defer testChartFree(h)

	bad := `{"replicaCount":-2}`
	code, detail = testSchemaValidate(h, &bad)
	assert.EqualValues(t, cerrors.CodeValues, code)
	assert.NotEmpty(t, detail)
}

func TestRegistryClientLifecycleThroughCAPI(t *testing.T) {
	before := registry.Count()

	code, h, detail := testRegistryClientNew(nil)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	require.NotZero(t, h)
	assert.EqualValues(t, before+1, registry.Count())

	// A registry-client handle is not freeable as a chart.
	chartCode, _ := testChartFree(h)
	assert.EqualValues(t, cerrors.CodeWrongHandleType, chartCode)

	code, detail = testRegistryClientFree(h)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.EqualValues(t, before, registry.Count())

	code, _ = testRegistryClientFree(h)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code)
}

func TestRegistryClientBadOptsThroughCAPI(t *testing.T) {
	bad := `{"nope":true}`
	code, h, detail := testRegistryClientNew(&bad)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
	assert.Zero(t, h)
	assert.NotEmpty(t, detail)
}

func TestConfigAndListThroughCAPI(t *testing.T) {
	opts := `{"kubeconfig_path":` + strconv.Quote(fixtureKubeconfig) + `,"storage_driver":"memory"}`
	code, h, detail := testConfigNew(&opts)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	require.NotZero(t, h)

	// The fixture kubeconfig points at an unreachable server; actions check
	// reachability first, so the failure crosses the ABI as a defined error.
	code, _, detail = testList(h, nil)
	assert.EqualValues(t, cerrors.CodeRelease, code)
	assert.Contains(t, detail, "unreachable")

	code, detail = testConfigFree(h)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	code, _ = testConfigFree(h)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code)

	// List on the freed handle is a defined error.
	code, _, _ = testList(h, nil)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code)
}

func TestConfigNewBadOptsThroughCAPI(t *testing.T) {
	bad := `{"bogus":true}`
	code, h, detail := testConfigNew(&bad)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
	assert.Zero(t, h)
	assert.NotEmpty(t, detail)
}

func TestContextLifecycleThroughCAPI(t *testing.T) {
	newCode, cancelCode, freeCode, doubleFreeCode := testContextLifecycle()
	assert.EqualValues(t, cerrors.CodeOK, newCode)
	assert.EqualValues(t, cerrors.CodeOK, cancelCode)
	assert.EqualValues(t, cerrors.CodeOK, freeCode)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, doubleFreeCode)
}

func TestLogHandlerBridge(t *testing.T) {
	testLogReset()
	require.EqualValues(t, cerrors.CodeOK, testLogHandlerInstall(0)) // HELM_LOG_DEBUG
	defer func() {
		require.EqualValues(t, cerrors.CodeOK, testLogHandlerClear())
	}()

	// Log through the installed handler exactly as a config would.
	logger := slog.New(wrapper.CurrentLogHandler())
	logger.Info("bridge check", "key", "value")

	count, level, msg := testLogCaptured()
	assert.EqualValues(t, 1, count, "exactly one record forwarded")
	assert.EqualValues(t, 1, level, "slog Info maps to HELM_LOG_INFO")
	assert.Contains(t, msg, "bridge check")
	assert.Contains(t, msg, "key=value")

	// Below min level: filtered out before reaching C.
	testLogReset()
	require.EqualValues(t, cerrors.CodeOK, testLogHandlerInstall(3)) // HELM_LOG_ERROR
	logger = slog.New(wrapper.CurrentLogHandler())
	logger.Warn("too quiet")
	count, _, _ = testLogCaptured()
	assert.EqualValues(t, 0, count, "records below min_level are dropped")

	logger.Error("loud enough")
	count, level, _ = testLogCaptured()
	assert.EqualValues(t, 1, count)
	assert.EqualValues(t, 3, level, "slog Error maps to HELM_LOG_ERROR")

	// Clearing restores silence.
	testLogReset()
	require.EqualValues(t, cerrors.CodeOK, testLogHandlerClear())
	slog.New(wrapper.CurrentLogHandler()).Error("into the void")
	count, _, _ = testLogCaptured()
	assert.EqualValues(t, 0, count)
}

func TestChartFreeWrongType(t *testing.T) {
	id := registry.Put(handles.TypeContext, "not a chart")
	code, detail := testChartFree(id)
	assert.EqualValues(t, cerrors.CodeWrongHandleType, code)
	assert.NotEmpty(t, detail)
	// The wrongly-typed free must not have removed it; clean up generically.
	code, _ = testHandleFree(id, false)
	assert.EqualValues(t, cerrors.CodeOK, code)
}
