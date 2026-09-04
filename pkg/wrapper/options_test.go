package wrapper

// Option decoding and small-helper coverage: every opts_json decoder must
// accept empty input, decode a representative document, and reject an
// unknown key (docs/DESIGN.md §4).

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/downloader"
	rspb "helm.sh/helm/v4/pkg/release/v1"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

func TestEveryOptionsDecoder(t *testing.T) {
	decoders := map[string]func(string) (any, error){
		"install":       func(s string) (any, error) { return ParseInstallOptions(s) },
		"upgrade":       func(s string) (any, error) { return ParseUpgradeOptions(s) },
		"uninstall":     func(s string) (any, error) { return ParseUninstallOptions(s) },
		"rollback":      func(s string) (any, error) { return ParseRollbackOptions(s) },
		"list":          func(s string) (any, error) { return ParseListOptions(s) },
		"status":        func(s string) (any, error) { return ParseStatusOptions(s) },
		"history":       func(s string) (any, error) { return ParseHistoryOptions(s) },
		"get":           func(s string) (any, error) { return ParseGetOptions(s) },
		"get metadata":  func(s string) (any, error) { return ParseGetMetadataOptions(s) },
		"get values":    func(s string) (any, error) { return ParseGetValuesOptions(s) },
		"test":          func(s string) (any, error) { return ParseTestOptions(s) },
		"config":        func(s string) (any, error) { return ParseConfigOptions(s) },
		"render":        func(s string) (any, error) { return ParseRenderOptions(s) },
		"lint":          func(s string) (any, error) { return ParseLintOptions(s) },
		"package":       func(s string) (any, error) { return ParsePackageOptions(s) },
		"sign":          func(s string) (any, error) { return ParseSignOptions(s) },
		"show":          func(s string) (any, error) { return ParseShowOptions(s) },
		"pull":          func(s string) (any, error) { return ParsePullOptions(s) },
		"push":          func(s string) (any, error) { return ParsePushOptions(s) },
		"repo index":    func(s string) (any, error) { return ParseRepoIndexOptions(s) },
		"repo generate": func(s string) (any, error) { return ParseRepoIndexGenerateOptions(s) },
		"dependency":    func(s string) (any, error) { return ParseDependencyOptions(s) },
		"registry":      func(s string) (any, error) { return ParseRegistryClientOptions(s) },
		"login":         func(s string) (any, error) { return ParseLoginOptions(s) },
	}
	for name, decode := range decoders {
		t.Run(name, func(t *testing.T) {
			_, err := decode("")
			assert.NoError(t, err, "empty input is all defaults")
			_, err = decode("{}")
			assert.NoError(t, err)
			_, err = decode(`{"this_key_does_not_exist":1}`)
			assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err), "unknown keys fail loudly")
			_, err = decode("{not json")
			assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
		})
	}
}

func TestOptionDocumentsRoundTrip(t *testing.T) {
	inst, err := ParseInstallOptions(`{"namespace":"ns","dry_run":"client","labels":{"a":"b"},"server_side_apply":false,
		"api_versions":["x/v1"],"chart_repo_url":"https://r","dependency_update":true}`)
	require.NoError(t, err)
	assert.Equal(t, "ns", inst.Namespace)
	assert.NotNil(t, inst.ServerSideApply)
	assert.False(t, *inst.ServerSideApply)
	assert.Equal(t, []string{"x/v1"}, inst.APIVersions)
	assert.True(t, inst.DependencyUpdate)
	assert.Equal(t, "https://r", inst.ChartRepoURL)

	up, err := ParseUpgradeOptions(`{"server_side_apply":"auto","reset_then_reuse_values":true,"max_history":3}`)
	require.NoError(t, err)
	assert.Equal(t, "auto", up.ServerSideApply)
	assert.True(t, up.ResetThenReuseValues)
	assert.Equal(t, 3, up.MaxHistory)

	un, err := ParseUninstallOptions(`{"deletion_propagation":"foreground","disable_hooks":true}`)
	require.NoError(t, err)
	assert.Equal(t, "foreground", un.DeletionPropagation)

	rb, err := ParseRollbackOptions(`{"version":2,"cleanup_on_fail":true,"max_history":5}`)
	require.NoError(t, err)
	assert.Equal(t, 2, rb.Version)

	ls, err := ParseListOptions(`{"deployed":true,"failed":true,"selector":"a=b","by_date":true}`)
	require.NoError(t, err)
	assert.True(t, ls.Deployed && ls.Failed && ls.ByDate)
	assert.Equal(t, "a=b", ls.Selector)

	tst, err := ParseTestOptions(`{"logs":true,"include_names":["a"],"exclude_names":["b"],"timeout_seconds":9}`)
	require.NoError(t, err)
	assert.True(t, tst.Logs)
	assert.Equal(t, []string{"a"}, tst.IncludeNames)

	show, err := ParseShowOptions(`{"format":"readme","devel":true,"plain_http":true}`)
	require.NoError(t, err)
	assert.Equal(t, "readme", show.Format)
	assert.True(t, show.PlainHTTP)

	gen, err := ParseRepoIndexGenerateOptions(`{"base_url":"https://x","merge":"m.yaml","json":true}`)
	require.NoError(t, err)
	assert.True(t, gen.JSON)

	dep, err := ParseDependencyOptions(`{"verify_mode":"later","debug":true,"username":"u"}`)
	require.NoError(t, err)
	assert.Equal(t, "later", dep.VerifyMode)

	login, err := ParseLoginOptions(`{"cert_file":"c","key_file":"k","ca_file":"a"}`)
	require.NoError(t, err)
	assert.Equal(t, "c", login.CertFile)

	sign, err := ParseSignOptions(`{"key":"me","keyring":"ring","passphrase_file":"p"}`)
	require.NoError(t, err)
	assert.Equal(t, "me", sign.Key)
}

func TestVerificationStrategyValues(t *testing.T) {
	cases := map[string]downloader.VerificationStrategy{
		"": downloader.VerifyNever, "never": downloader.VerifyNever, "false": downloader.VerifyNever,
		"always": downloader.VerifyAlways, "true": downloader.VerifyAlways,
		"if_possible": downloader.VerifyIfPossible, "later": downloader.VerifyLater,
	}
	for in, want := range cases {
		got, err := verificationStrategy(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	_, err := verificationStrategy("sometimes")
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))

	m, dir, err := dependencyManager(testChart, DependencyOptions{VerifyMode: "if_possible", Debug: true})
	require.NoError(t, err)
	defer removeBestEffort(dir)
	assert.Equal(t, downloader.VerifyIfPossible, m.Verify)
	assert.True(t, m.Debug)
	_, _, err = dependencyManager(testChart, DependencyOptions{VerifyMode: "nope"})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
}

func TestDependencyStatusHelpers(t *testing.T) {
	assert.Equal(t, "", versionStatus("1.2.3", "1.2.3"))
	assert.Equal(t, "", versionStatus("1.2.4", "^1.2.0"))
	assert.Equal(t, "wrong version", versionStatus("2.0.0", "^1.2.0"))
	assert.Equal(t, "invalid version", versionStatus("1.2.3", "not a constraint ^^"))
	assert.Equal(t, "invalid version", versionStatus("banana", "^1.0.0"))

	found := versionedArchives([]string{"/x/sub-1.0.0.tgz", "/x/sub-latest.tgz", "/x/sub-2.0.0.tgz"}, "sub")
	assert.Equal(t, []string{"/x/sub-1.0.0.tgz", "/x/sub-2.0.0.tgz"}, found)

	// Bogus archives make the parent itself unloadable (the SDK unpacks
	// charts/*.tgz on load, as the CLI does), so the per-archive statuses
	// are checked through the helpers that `helm dependency list` uses.
	dir := writeParentChart(t, "http://127.0.0.1:1")
	charts := filepath.Join(dir, "charts")
	require.NoError(t, os.MkdirAll(charts, 0o755))
	dep := &chart.Dependency{Name: "testchart", Version: "0.1.0"}
	for _, v := range []string{"0.1.0", "0.2.0"} {
		require.NoError(t, os.WriteFile(filepath.Join(charts, "testchart-"+v+".tgz"), []byte("x"), 0o600))
	}
	assert.Equal(t, "too many matches", packagedStatus(dir, dep))
	require.NoError(t, os.Remove(filepath.Join(charts, "testchart-0.2.0.tgz")))
	assert.Equal(t, "corrupt", packagedStatus(dir, dep))
	assert.Equal(t, "", archiveStatus(filepath.Join(charts, "absent.tgz"), &chart.Dependency{Name: "absent"}))
	_, err := DependencyList(dir)
	assert.Equal(t, cerrors.CodeChartLoad, cerrors.FromError(err), "parent with a corrupt subchart archive")

	// An unpacked subchart directory satisfying the constraint.
	sub := writeParentWithSubchart(t)
	out, err := DependencyList(sub)
	require.NoError(t, err)
	assert.Contains(t, out, `"status":"unpacked"`)
}

func TestPostRendererUnknownPluginIsInvalidArg(t *testing.T) {
	pr, err := postRendererFor("", nil)
	require.NoError(t, err)
	assert.Nil(t, pr)
	_, err = postRendererFor("no-such-helm-plugin-xyz", []string{"a"})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(validateServerSideApply("perhaps")))
}

func TestTestHelpers(t *testing.T) {
	rel := &rspb.Release{Hooks: []*rspb.Hook{{Events: []rspb.HookEvent{rspb.HookPreInstall}}}}
	assert.False(t, hasTestHooks(rel))
	rel.Hooks = append(rel.Hooks, &rspb.Hook{Events: []rspb.HookEvent{rspb.HookTest}})
	assert.True(t, hasTestHooks(rel))

	cfg := newMemoryConfig(t)
	logs, err := collectTestLogs(cfg, nil, rel, false)
	require.NoError(t, err)
	assert.Empty(t, logs, "not wanted: nothing fetched")
	_, err = collectTestLogs(cfg, nil, rel, true)
	assert.Equal(t, cerrors.CodeKube, cerrors.FromError(err), "no REST getter: typed error, never a nil dereference")

	assert.Nil(t, firstError(nil, nil))
	e := cerrors.New(cerrors.CodeIO, "x")
	assert.Same(t, e, firstError(e, cerrors.New(cerrors.CodeIO, "y")))
}

func TestRenderWithConfigNeedsClusterConnection(t *testing.T) {
	cfg := newMemoryConfig(t) // no REST getter
	opts, err := ParseRenderOptions("")
	require.NoError(t, err)
	_, err = RenderChartWithConfig(cfg, loadFixtureChart(t), "", opts)
	assert.Equal(t, cerrors.CodeKube, cerrors.FromError(err))

	// With a real (unreachable) cluster config the chart check applies.
	real, err := NewConfig(ConfigOptions{KubeconfigPath: kubeconfigPath, StorageDriver: "memory"})
	require.NoError(t, err)
	defer real.Close()
	_, err = RenderChartWithConfig(real, "not a chart", "", opts)
	assert.Equal(t, cerrors.CodeWrongHandleType, cerrors.FromError(err))
	out, err := RenderChartWithConfig(real, loadFixtureChart(t), "", opts)
	require.NoError(t, err, "only lookup contacts the cluster; the fixture never calls it")
	assert.Contains(t, out, "release-name-config")
}

func TestChartCRDsListed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crdchart")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "crds"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: crdchart\nversion: 0.1.0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "crds", "thing.yaml"),
		[]byte("apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: things.example.io\n"), 0o644))
	c, err := LoadChart(dir)
	require.NoError(t, err)
	out, err := ChartCRDsJSON(c)
	require.NoError(t, err)
	assert.Contains(t, out, `"name":"crds/thing.yaml"`)
	assert.Contains(t, out, "things.example.io")
}

func TestShowRemoteChartThroughRepo(t *testing.T) {
	srv := startRepoServer(t)
	out, err := ShowChart(nil, "testchart", ShowOptions{Format: "chart",
		ChartRefOptions: ChartRefOptions{ChartRepoURL: srv.URL(), ChartVersion: "0.1.0"}})
	require.NoError(t, err)
	assert.Contains(t, out, "name: testchart")

	_, err = ShowChart(nil, "no-such-chart", ShowOptions{ChartRefOptions: ChartRefOptions{ChartRepoURL: srv.URL()}})
	assert.Equal(t, cerrors.CodeChartLoad, cerrors.FromError(err))
	_, err = ShowChart(nil, "neither-local-nor-remote", ShowOptions{})
	assert.Equal(t, cerrors.CodeChartLoad, cerrors.FromError(err))
}

func TestCloseConfigTolerates(t *testing.T) {
	CloseConfig("not a config") // must be a no-op
	cfg, err := NewConfig(ConfigOptions{KubeconfigContent: "apiVersion: v1\nkind: Config\n", StorageDriver: "memory"})
	require.NoError(t, err)
	path := cfg.tempKubeconfig
	assert.FileExists(t, path)
	CloseConfig(cfg)
	assert.NoFileExists(t, path)
	cfg.Close() // idempotent

	// The kube error path of an action on a config with no reachable cluster.
	_, err = InstallRelease(context.Background(), cfg, loadFixtureChart(t), "", "x", "", InstallOptions{})
	assert.Equal(t, cerrors.CodeKube, cerrors.FromError(err))
}
