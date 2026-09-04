package main

// Boundary tests for the symbols added after 0.1.0, so every //export is
// exercised through the real conversion layer. Network/cluster shims are
// driven through their offline-reachable paths.

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

func strp(s string) *string { return &s }

func TestChartContentShims(t *testing.T) {
	h := loadFixtureChartHandle(t)

	code, out, detail := testChartTemplates(h)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, out, "templates/configmap.yaml")

	code, out, _ = testChartFiles(h)
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Equal(t, "[]", out)

	code, out, _ = testChartCRDs(h)
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Equal(t, "[]", out)

	code, out, _ = testChartSchema(h)
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Equal(t, "null", out)

	code, out, _ = testChartDependencies(h)
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Equal(t, "[]", out)

	// Wrong handle type and freed handle are defined errors.
	code, _, detail = testChartFiles(987654)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code)
	assert.NotEmpty(t, detail)
}

func TestChartArchiveShims(t *testing.T) {
	h := loadFixtureChartHandle(t)
	dest := t.TempDir()
	code, tgz, detail := testChartSave(h, dest)
	require.EqualValues(t, cerrors.CodeOK, code, detail)

	data, err := os.ReadFile(tgz)
	require.NoError(t, err)
	code, h2, detail := testChartLoadArchive(data)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	require.NotZero(t, h2)
	code, meta, _ := testChartMetadata(h2)
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Contains(t, meta, `"name":"testchart"`)
	testChartFree(h2)

	code, _, _ = testChartLoadArchive(nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code, "NULL buffer rejected")
	code, _, _ = testChartLoadArchive([]byte("garbage"))
	assert.EqualValues(t, cerrors.CodeChartLoad, code)

	code, digest, _ := testChartDigest(strp(tgz))
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, digest)
	code, _, _ = testChartDigest(nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)

	expanded := t.TempDir()
	code, detail = testChartExpand(strp(expanded), strp(tgz))
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.FileExists(t, filepath.Join(expanded, "testchart", "Chart.yaml"))
	code, _ = testChartExpand(strp(expanded), nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)

	code, dir, detail := testChartSaveDir(h, strp(t.TempDir()))
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.FileExists(t, filepath.Join(dir, "Chart.yaml"))

	code, created, detail := testChartCreateFrom(strp("fromstarter"), strp(t.TempDir()), strp(fixtureChart))
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.FileExists(t, filepath.Join(created, "Chart.yaml"))
	code, _, _ = testChartCreateFrom(strp("x"), nil, strp(fixtureChart))
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
}

func TestSignShim(t *testing.T) {
	h := loadFixtureChartHandle(t)
	code, tgz, detail := testChartSave(h, t.TempDir())
	require.EqualValues(t, cerrors.CodeOK, code, detail)

	opts := `{"key":"helm-c-sdk-test","keyring":` + strconv.Quote(filepath.Join(fixtureSigning, "secring.gpg")) + `}`
	code, prov, detail := testChartSign(strp(tgz), strp(opts))
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Equal(t, tgz+".prov", prov)

	code, _, _ = testChartSign(strp(tgz), nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code, "key and keyring are required")
	bad := `{"nope":1}`
	code, _, _ = testChartSign(strp(tgz), &bad)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
}

func TestValuesAndStrvalsShims(t *testing.T) {
	code, out, detail := testValuesFromYAML(strp("a: 1\n"))
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Equal(t, `{"a":1}`, out)
	code, _, _ = testValuesFromYAML(strp("a: [oops"))
	assert.EqualValues(t, cerrors.CodeValues, code)

	code, out, _ = testStrvalsString(strp("p=80"))
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Equal(t, `{"p":"80"}`, out)
	code, out, _ = testStrvalsJSON(strp(`a={"b":1}`))
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Equal(t, `{"a":{"b":1}}`, out)
	code, out, _ = testStrvalsLiteral(strp("a=b,c"))
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Equal(t, `{"a":"b,c"}`, out)

	f := filepath.Join(t.TempDir(), "v")
	require.NoError(t, os.WriteFile(f, []byte("filed"), 0o600))
	// --set-file reads its value with --set escaping rules, where a
	// backslash escapes the next character; a Windows path must therefore
	// be given with forward slashes, exactly as with the helm CLI.
	code, out, _ = testStrvalsFile(strp("k=" + filepath.ToSlash(f)))
	assert.EqualValues(t, cerrors.CodeOK, code)
	assert.Equal(t, `{"k":"filed"}`, out)

	code, _, _ = testStrvalsString(nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
	code, _, _ = testStrvalsJSON(strp("a={not json"))
	assert.EqualValues(t, cerrors.CodeValues, code)
}

func TestShowAndLintOptsShims(t *testing.T) {
	code, out, detail := testShow(0, strp(fixtureChart), strp(`{"format":"chart"}`))
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, out, "name: testchart")
	code, _, _ = testShow(0, strp(fixtureChart), strp(`{"format":"poster"}`))
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
	code, _, _ = testShow(0, nil, nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)

	code, report, detail := testLintRunOpts(fixtureChart, nil, strp(`{"strict":true,"kube_version":"v1.30.0"}`))
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, report, "total_charts_linted")
	code, _, _ = testLintRunOpts(fixtureChart, nil, strp(`{"kube_version":"nope"}`))
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
}

func TestRepoIndexAndDependencyListShims(t *testing.T) {
	h := loadFixtureChartHandle(t)
	dir := t.TempDir()
	code, _, detail := testChartSave(h, dir)
	require.EqualValues(t, cerrors.CodeOK, code, detail)

	code, idx, detail := testRepoIndexGenerate(strp(dir), strp(`{"base_url":"https://x/"}`))
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, idx, `"testchart"`)
	assert.FileExists(t, filepath.Join(dir, "index.yaml"))
	code, _, _ = testRepoIndexGenerate(nil, nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)

	code, deps, detail := testDependencyList(strp(fixtureChart))
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Equal(t, "[]", deps)
	code, _, _ = testDependencyList(strp("/no/such/chart"))
	assert.EqualValues(t, cerrors.CodeChartLoad, code)
}

func TestRegistryTagShimsOffline(t *testing.T) {
	code, h, detail := testRegistryClientNew(strp(`{"plain_http":true}`))
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	defer testRegistryClientFree(h)

	code, _, detail = testRegistryTags(h, strp("oci://127.0.0.1:1/x/y"))
	assert.EqualValues(t, cerrors.CodeRegistry, code)
	assert.NotEmpty(t, detail)
	code, _, _ = testRegistryResolve(h, strp("oci://127.0.0.1:1/x/y:1.0.0"))
	assert.EqualValues(t, cerrors.CodeRegistry, code)
	code, _, _ = testRegistryTags(0, strp("oci://127.0.0.1:1/x/y"))
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code, "a client is required")
	code, _, _ = testRegistryTags(h, nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
}

func TestConfigExtrasAndReleaseShims(t *testing.T) {
	cfg := newMemoryConfigHandle(t)
	code, client, detail := testRegistryClientNew(nil)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	defer testRegistryClientFree(client)

	code, detail = testConfigSetRegistryClient(cfg, client)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	code, _ = testConfigSetRegistryClient(cfg, 0)
	assert.EqualValues(t, cerrors.CodeOK, code, "0 unbinds")
	code, _ = testConfigSetRegistryClient(cfg, 987654)
	assert.EqualValues(t, cerrors.CodeInvalidHandle, code)

	// The fixture cluster is unreachable: a kube error, never a hang.
	code, detail = testConfigCheckReachable(cfg)
	assert.EqualValues(t, cerrors.CodeKube, code)
	assert.NotEmpty(t, detail)

	code, _, detail = testGet(cfg, "absent", nil)
	assert.NotEqualValues(t, cerrors.CodeOK, code)
	assert.NotEmpty(t, detail)
	code, _, _ = testTestRun(cfg, "absent", nil)
	assert.NotEqualValues(t, cerrors.CodeOK, code)
	bad := `{"bogus":1}`
	code, _, _ = testTestRun(cfg, "absent", &bad)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)

	// A cluster-aware render only contacts the API server from `lookup`;
	// the fixture chart never calls it, so this succeeds even though the
	// fixture cluster is unreachable — the REST config alone must resolve.
	chart := loadFixtureChartHandle(t)
	code, out, detail := testRenderWithConfig(cfg, chart, nil, strp(`{"name":"cfg-render"}`))
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, out, "cfg-render")
	code, _, _ = testRenderWithConfig(cfg, 0, nil, nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
}
