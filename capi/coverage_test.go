package main

// Boundary tests for the shims not covered in capi_test.go, so that every
// //export symbol is exercised through the real conversion layer. Cluster and
// network shims are driven through their offline-reachable paths (argument
// validation, unreachable endpoints, not-logged-in) — their happy paths run
// in the wrapper integration suites and the kind e2e job.

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

func loadFixtureChartHandle(t *testing.T) uint64 {
	t.Helper()
	code, h, detail := testChartLoad(fixtureChart)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	t.Cleanup(func() { testChartFree(h) })
	return h
}

func newMemoryConfigHandle(t *testing.T) uint64 {
	t.Helper()
	opts := `{"kubeconfig_path":` + strconv.Quote(fixtureKubeconfig) + `,"storage_driver":"memory"}`
	code, h, detail := testConfigNew(&opts)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	t.Cleanup(func() { testConfigFree(h) })
	return h
}

func TestChartValuesSaveThroughCAPI(t *testing.T) {
	h := loadFixtureChartHandle(t)

	code, vals, detail := testChartValues(h)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, vals, "replicaCount")

	dest := t.TempDir()
	code, tgz, detail := testChartSave(h, dest)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, tgz, "testchart-0.1.0.tgz")
}

func TestChartCreateThroughCAPI(t *testing.T) {
	dir := t.TempDir()
	code, path, detail := testChartCreate("capichart", dir)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, path, "capichart")

	code, _, detail = testChartCreate("Bad Name!", dir)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
	assert.NotEmpty(t, detail)
}

func TestLintAndPackageThroughCAPI(t *testing.T) {
	code, report, detail := testLintRun(fixtureChart, nil)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, report, "total_charts_linted")

	bad := "{not json"
	code, _, _ = testLintRun(fixtureChart, &bad)
	assert.EqualValues(t, cerrors.CodeValues, code)

	opts := `{"destination":` + strconv.Quote(t.TempDir()) + `}`
	code, tgz, detail := testPackageRun(fixtureChart, &opts)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, tgz, ".tgz")

	unknown := `{"nope":1}`
	code, _, _ = testPackageRun(fixtureChart, &unknown)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
}

func TestChartVerifyThroughCAPI(t *testing.T) {
	tgz := filepath.Join(fixtureSigning, "testchart-0.1.0.tgz")
	keyring := filepath.Join(fixtureSigning, "pubring.gpg")

	code, out, detail := testChartVerify(tgz, nil, keyring)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	assert.Contains(t, out, "signed_by")

	code, _, _ = testChartVerify("no-such.tgz", nil, keyring)
	assert.EqualValues(t, cerrors.CodeChartInvalid, code)
}

func TestOpenHandlesCountThroughCAPI(t *testing.T) {
	before := testOpenHandlesCount()
	h := loadFixtureChartHandle(t)
	assert.Equal(t, before+1, testOpenHandlesCount())
	testChartFree(h)
	assert.Equal(t, before, testOpenHandlesCount())
}

func TestReleaseActionShimsAgainstUnreachableCluster(t *testing.T) {
	cfg := newMemoryConfigHandle(t)

	checks := []struct {
		name string
		run  func() (int32, string)
	}{
		{"status", func() (int32, string) { c, _, d := testStatus(cfg, "absent", nil); return c, d }},
		{"history", func() (int32, string) { c, _, d := testHistory(cfg, "absent", nil); return c, d }},
		{"get_values", func() (int32, string) { c, _, d := testGetValues(cfg, "absent", nil); return c, d }},
		{"get_metadata", func() (int32, string) { c, _, d := testGetMetadata(cfg, "absent", nil); return c, d }},
		{"uninstall", func() (int32, string) { c, _, d := testUninstall(cfg, "absent", nil); return c, d }},
		{"rollback", func() (int32, string) { c, d := testRollback(cfg, "absent", nil); return c, d }},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			code, detail := tc.run()
			assert.NotEqualValues(t, cerrors.CodeOK, code,
				"action against an unreachable cluster must fail cleanly")
			assert.NotEmpty(t, detail)
		})
	}
}

func TestInstallUpgradeShimArgumentPaths(t *testing.T) {
	cfg := newMemoryConfigHandle(t)
	chart := loadFixtureChartHandle(t)

	// Neither chart handle nor chart_ref: invalid arguments, through the ABI.
	code, _, detail := testInstall(cfg, 0, 0, nil, "x-rel", nil, nil)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
	assert.NotEmpty(t, detail)

	// With a chart handle, the shim plumbs through to the SDK; the fixture
	// cluster is unreachable, so a clean failure (never a hang or crash).
	code, _, _ = testInstall(cfg, 0, chart, nil, "x-rel", nil, nil)
	assert.NotEqualValues(t, cerrors.CodeOK, code)

	code, _, _ = testUpgrade(cfg, 0, chart, nil, "x-rel", nil, nil)
	assert.NotEqualValues(t, cerrors.CodeOK, code)

	// Bad opts JSON is rejected before any cluster contact.
	bad := `{"bogus":true}`
	code, _, _ = testInstall(cfg, 0, chart, nil, "x-rel", nil, &bad)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
	code, _, _ = testUpgrade(cfg, 0, chart, nil, "x-rel", nil, &bad)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
}

func TestDistributionShimsOffline(t *testing.T) {
	code, h, detail := testRegistryClientNew(nil)
	require.EqualValues(t, cerrors.CodeOK, code, detail)
	defer testRegistryClientFree(h)

	// Login to an unreachable registry: clean failure.
	opts := `{"plain_http":true,"insecure":true}`
	code, detail = testRegistryLogin(h, "127.0.0.1:1", "u", "p", &opts)
	assert.NotEqualValues(t, cerrors.CodeOK, code)
	assert.NotEmpty(t, detail)

	// Logout without a login is environment-dependent: oras-go >= v2.6.2
	// treats removing absent credentials from a FILE store as a no-op (OK),
	// while OS-keychain credential helpers still surface an error. Either
	// way it must return a defined code, never crash.
	code, _ = testRegistryLogout(h, "127.0.0.1:1")
	assert.Contains(t, []int32{int32(cerrors.CodeOK), int32(cerrors.CodeRegistry)}, int32(code),
		"logout without login must be OK or a defined registry error")

	// Pull from an unreachable repo; push of a nonexistent archive.
	pullOpts := `{"repo_url":"http://127.0.0.1:1","dest_dir":` + strconv.Quote(t.TempDir()) + `}`
	code, _, _ = testPull(0, "somechart", &pullOpts)
	assert.EqualValues(t, cerrors.CodeRepo, code)

	pushOpts := `{"plain_http":true}`
	code, _, _ = testPush(h, "no-such-chart.tgz", "oci://127.0.0.1:1/repo", &pushOpts)
	assert.NotEqualValues(t, cerrors.CodeOK, code)

	// Repo index from an unreachable server.
	code, _, _ = testRepoIndexDownload("http://127.0.0.1:1", nil)
	assert.EqualValues(t, cerrors.CodeRepo, code)
}

func TestDependencyShimsThroughCAPI(t *testing.T) {
	// A scaffold chart has no dependencies: update and build succeed offline.
	dir := t.TempDir()
	code, path, detail := testChartCreate("depchart", dir)
	require.EqualValues(t, cerrors.CodeOK, code, detail)

	code, detail = testDependencyUpdate(path, nil)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)
	code, detail = testDependencyBuild(path, nil)
	assert.EqualValues(t, cerrors.CodeOK, code, detail)

	bad := `{"nope":1}`
	code, _ = testDependencyUpdate(path, &bad)
	assert.EqualValues(t, cerrors.CodeInvalidArg, code)
}
