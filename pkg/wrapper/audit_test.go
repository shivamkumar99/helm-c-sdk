package wrapper

// Regression tests for the defects found by the SDK-coverage audit, plus
// coverage for the capabilities it added. Each test names the finding it
// pins down so a future regression points straight at its cause.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"helm.sh/helm/v4/pkg/action"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// --- F-3: dry_run validation -------------------------------------------------

func TestParseDryRunMirrorsTheCLI(t *testing.T) {
	cases := map[string]action.DryRunStrategy{
		"": action.DryRunNone, "none": action.DryRunNone,
		"client": action.DryRunClient, "server": action.DryRunServer,
		"true": action.DryRunClient, "false": action.DryRunNone,
	}
	for in, want := range cases {
		got, err := parseDryRun(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"yes", "Client", "template", "1x"} {
		_, err := parseDryRun(bad)
		assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err), bad)
	}
}

func TestUnknownDryRunIsRejectedBeforeAnyWork(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := InstallRelease(context.Background(), cfg, loadFixtureChart(t), "", "dr-rel", "",
		InstallOptions{DryRun: "bogus"})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(
		RollbackRelease(cfg, "dr-rel", RollbackOptions{DryRun: "bogus"})))
}

// --- M-1: client-side dry run must not corrupt the shared configuration --------

func TestClientDryRunLeavesSharedConfigIntact(t *testing.T) {
	cfg := newMemoryConfig(t)
	kubeBefore, storeBefore, capsBefore := cfg.Cfg.KubeClient, cfg.Cfg.Releases, cfg.Cfg.Capabilities

	out, err := InstallRelease(context.Background(), cfg, loadFixtureChart(t), "", "dry-rel", "",
		InstallOptions{DryRun: "client"})
	require.NoError(t, err)
	assert.Equal(t, "pending-install", mustSummary(t, out).Status)

	// The SDK swaps in a discarding kube client and an empty memory store
	// for a client dry run; that must have happened on a throwaway copy.
	assert.Same(t, kubeBefore, cfg.Cfg.KubeClient, "kube client replaced on the shared config")
	assert.Same(t, storeBefore, cfg.Cfg.Releases, "release storage replaced on the shared config")
	assert.Same(t, capsBefore, cfg.Cfg.Capabilities, "capabilities replaced on the shared config")
	_, isFake := cfg.Cfg.KubeClient.(*kubefake.PrintingKubeClient)
	assert.False(t, isFake)

	// A real install afterwards still reaches the real store.
	_, err = InstallRelease(context.Background(), cfg, loadFixtureChart(t), "", "real-rel", "", InstallOptions{})
	require.NoError(t, err)
	list, err := ListReleases(cfg, ListOptions{})
	require.NoError(t, err)
	assert.Contains(t, list, `"name":"real-rel"`)
	assert.NotContains(t, list, `"name":"dry-rel"`, "a dry run persists nothing")
}

// --- M-2 / F-2: declared dependencies --------------------------------------

// writeParentWithSubchart writes a parent chart whose subchart lives under
// charts/ and is gated by the condition sub.enabled.
func writeParentWithSubchart(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "parent")
	files := map[string]string{
		"Chart.yaml": `apiVersion: v2
name: parent
version: 0.1.0
dependencies:
  - name: sub
    version: "0.1.0"
    repository: ""
    condition: sub.enabled
`,
		"values.yaml":                   "sub:\n  enabled: true\n",
		"templates/parent.yaml":         "kind: ConfigMap\nmetadata:\n  name: parent-{{ .Release.Name }}\n",
		"charts/sub/Chart.yaml":         "apiVersion: v2\nname: sub\nversion: 0.1.0\n",
		"charts/sub/templates/sub.yaml": "kind: ConfigMap\nmetadata:\n  name: sub-{{ .Release.Name }}\n",
		"charts/sub/values.yaml":        "{}\n",
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}
	return dir
}

func TestInstallRejectsMissingDeclaredDependency(t *testing.T) {
	dir := writeParentChart(t, "http://127.0.0.1:1") // declares testchart, charts/ absent
	cfg := newMemoryConfig(t)
	_, err := InstallRelease(context.Background(), cfg, nil, dir, "dep-rel", "", InstallOptions{})
	require.Error(t, err)
	assert.Equal(t, cerrors.CodeChartInvalid, cerrors.FromError(err))
	assert.Contains(t, err.Error(), "dependencies are missing")
}

func TestInstallAcceptsPresentDependency(t *testing.T) {
	dir := writeParentWithSubchart(t)
	cfg := newMemoryConfig(t)
	out, err := InstallRelease(context.Background(), cfg, nil, dir, "sub-rel", "", InstallOptions{})
	require.NoError(t, err)
	assert.Contains(t, mustSummary(t, out).Manifest, "sub-sub-rel", "enabled subchart rendered")
}

func TestRenderHonoursDependencyCondition(t *testing.T) {
	dir := writeParentWithSubchart(t)
	opts, err := ParseRenderOptions(`{"name":"r"}`)
	require.NoError(t, err)

	c, err := LoadChart(dir)
	require.NoError(t, err)
	out, err := RenderChart(c, `{"sub":{"enabled":false}}`, opts)
	require.NoError(t, err)
	var manifests map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &manifests))
	for k := range manifests {
		assert.NotContains(t, k, "charts/sub", "disabled subchart must not render")
	}

	c, err = LoadChart(dir) // reload: processing prunes the in-memory chart
	require.NoError(t, err)
	out, err = RenderChart(c, "", opts)
	require.NoError(t, err)
	assert.Contains(t, out, "sub-r", "enabled subchart renders")
}

// --- F-4 / F-5 / F-8 / M-3 -----------------------------------------------------

func TestHistoryMaxKeepsNewestRevisions(t *testing.T) {
	cfg := newMemoryConfig(t)
	chart := loadFixtureChart(t)
	ctx := context.Background()
	_, err := InstallRelease(ctx, cfg, chart, "", "hist-rel", "", InstallOptions{})
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = UpgradeRelease(ctx, cfg, chart, "", "hist-rel", "", UpgradeOptions{})
		require.NoError(t, err)
	}

	out, err := HistoryRelease(cfg, "hist-rel", HistoryOptions{Max: 2})
	require.NoError(t, err)
	var revs []summaryJSON
	require.NoError(t, json.Unmarshal([]byte(out), &revs))
	require.Len(t, revs, 2)
	assert.Equal(t, []int{2, 3}, []int{revs[0].Revision, revs[1].Revision}, "newest two, ascending")

	out, err = HistoryRelease(cfg, "hist-rel", HistoryOptions{})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &revs))
	assert.Len(t, revs, 3, "max 0 keeps everything")
}

func TestUpgradeNamespaceMismatchIsRejected(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := UpgradeRelease(context.Background(), cfg, loadFixtureChart(t), "", "x", "",
		UpgradeOptions{Namespace: "elsewhere"})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
}

func TestChartHandleAndRefTogetherAreRejected(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := InstallRelease(context.Background(), cfg, loadFixtureChart(t), "also-a-ref", "x", "", InstallOptions{})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
}

func TestListInvalidFilterIsInvalidArg(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := ListReleases(cfg, ListOptions{Filter: "("})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
}

func TestOptionValidation(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := UninstallRelease(cfg, "x", UninstallOptions{DeletionPropagation: "sideways"})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
	_, err = UpgradeRelease(context.Background(), cfg, loadFixtureChart(t), "", "x", "", UpgradeOptions{ServerSideApply: "maybe"})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
	_, err = InstallRelease(context.Background(), cfg, loadFixtureChart(t), "", "x", "", InstallOptions{renderTweaks: renderTweaks{PostRenderStrategy: "weird"}})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
	_, err = InstallRelease(context.Background(), cfg, loadFixtureChart(t), "", "x", "", InstallOptions{renderTweaks: renderTweaks{KubeVersion: "not-a-version"}})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
}

func TestServerSideApplyDefaultsSurvive(t *testing.T) {
	// Omitting the option must keep the SDK's defaults (install: true,
	// upgrade/rollback: "auto") rather than zeroing them.
	cfg := newMemoryConfig(t)
	inst := action.NewInstall(cfg.Cfg)
	require.NoError(t, applyInstallOptions(inst, InstallOptions{}))
	assert.True(t, inst.ServerSideApply)
	off := false
	require.NoError(t, applyInstallOptions(inst, InstallOptions{ServerSideApply: &off}))
	assert.False(t, inst.ServerSideApply)

	up := action.NewUpgrade(cfg.Cfg)
	require.NoError(t, applyUpgradeOptions(up, UpgradeOptions{}))
	assert.Equal(t, "auto", up.ServerSideApply)
}

// --- F-1 / F-6: sibling configurations ------------------------------------------

func TestForNamespaceDerivesSiblingWithoutTouchingShared(t *testing.T) {
	cfg, err := NewConfig(ConfigOptions{KubeconfigPath: kubeconfigPath, StorageDriver: "memory"})
	require.NoError(t, err)
	defer cfg.Close()

	same, err := cfg.forNamespace("default")
	require.NoError(t, err)
	assert.Same(t, cfg.Cfg, same)

	other, err := cfg.forNamespace("other")
	require.NoError(t, err)
	assert.NotSame(t, cfg.Cfg, other)
	assert.Equal(t, "default", cfg.Namespace, "shared config keeps its namespace")

	// all_namespaces takes the sibling path; the fixture cluster is
	// unreachable, which must surface as a kube error, not a release one.
	_, err = ListReleases(cfg, ListOptions{AllNamespaces: true})
	assert.Equal(t, cerrors.CodeKube, cerrors.FromError(err))
	assert.Same(t, same, cfg.Cfg, "shared config untouched by all_namespaces")
}

func TestUnreachableClusterIsKubeError(t *testing.T) {
	cfg, err := NewConfig(ConfigOptions{KubeconfigPath: kubeconfigPath, StorageDriver: "memory"})
	require.NoError(t, err)
	defer cfg.Close()
	assert.Equal(t, cerrors.CodeKube, cerrors.FromError(CheckReachable(cfg)))
	_, err = StatusRelease(cfg, "x", StatusOptions{})
	assert.Equal(t, cerrors.CodeKube, cerrors.FromError(err))
}

// --- new capabilities ------------------------------------------------------------

func TestStrvalsVariants(t *testing.T) {
	out, err := ParseSetStringValues("port=80")
	require.NoError(t, err)
	assert.Equal(t, `{"port":"80"}`, out)

	out, err = ParseSetString("port=80")
	require.NoError(t, err)
	assert.Equal(t, `{"port":80}`, out)

	out, err = ParseSetJSON(`a={"b":[1,2]}`)
	require.NoError(t, err)
	assert.Equal(t, `{"a":{"b":[1,2]}}`, out)

	out, err = ParseSetLiteral("a=b,c=d")
	require.NoError(t, err)
	assert.Equal(t, `{"a":"b,c=d"}`, out)

	f := filepath.Join(t.TempDir(), "v.txt")
	require.NoError(t, os.WriteFile(f, []byte("from-file"), 0o600))
	// --set escaping rules apply to the path: backslash escapes, so a
	// Windows path is given with forward slashes (as with the helm CLI).
	out, err = ParseSetFile("k=" + filepath.ToSlash(f))
	require.NoError(t, err)
	assert.Equal(t, `{"k":"from-file"}`, out)

	_, err = ParseSetFile("k=/no/such/file")
	assert.Equal(t, cerrors.CodeValues, cerrors.FromError(err))
}

func TestChartContentAccessors(t *testing.T) {
	c, err := LoadChart(schemaChart)
	require.NoError(t, err)

	tpl, err := ChartTemplatesJSON(c)
	require.NoError(t, err)
	assert.Contains(t, tpl, "templates/configmap.yaml")
	assert.Contains(t, tpl, ".Release.Name")

	schema, err := ChartSchemaJSON(c)
	require.NoError(t, err)
	assert.Contains(t, schema, `"replicaCount"`)

	files, err := ChartFilesJSON(c)
	require.NoError(t, err)
	assert.Equal(t, "[]", files, "fixture ships no extra files")

	crds, err := ChartCRDsJSON(c)
	require.NoError(t, err)
	assert.Equal(t, "[]", crds)

	deps, err := ChartDependenciesJSON(c)
	require.NoError(t, err)
	assert.Equal(t, "[]", deps)

	plain, err := LoadChart(testChart)
	require.NoError(t, err)
	schema, err = ChartSchemaJSON(plain)
	require.NoError(t, err)
	assert.Equal(t, "null", schema, "no schema is null, not an error")

	sub, err := LoadChart(writeParentWithSubchart(t))
	require.NoError(t, err)
	deps, err = ChartDependenciesJSON(sub)
	require.NoError(t, err)
	assert.Contains(t, deps, `"name":"sub"`)
}

func TestLoadChartArchiveFromBytes(t *testing.T) {
	data, err := os.ReadFile(packageFixture(t))
	require.NoError(t, err)
	c, err := LoadChartArchive(data)
	require.NoError(t, err)
	assert.Equal(t, "testchart", c.Name())

	_, err = LoadChartArchive([]byte("not a tarball"))
	assert.Equal(t, cerrors.CodeChartLoad, cerrors.FromError(err))
}

func TestExpandSaveDirAndCreateFrom(t *testing.T) {
	tgz := packageFixture(t)
	dest := t.TempDir()
	require.NoError(t, ExpandChartArchive(dest, tgz))
	assert.FileExists(t, filepath.Join(dest, "testchart", "Chart.yaml"))
	assert.Equal(t, cerrors.CodeChartLoad, cerrors.FromError(ExpandChartArchive(dest, "missing.tgz")))

	c, err := LoadChart(testChart)
	require.NoError(t, err)
	dir, err := SaveChartDir(c, t.TempDir())
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "values.yaml"))

	parent := t.TempDir()
	created, err := CreateChartFrom("fromstarter", parent, testChart)
	require.NoError(t, err)
	// The library must place the chart inside the requested directory —
	// assert containment before touching the returned path.
	require.Equal(t, parent, filepath.Dir(created), "created chart escapes the target directory")
	require.Equal(t, "fromstarter", filepath.Base(created))
	metaPath := filepath.Join(parent, filepath.Base(created), "Chart.yaml")
	assert.FileExists(t, metaPath)
	meta, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	assert.Contains(t, string(meta), "name: fromstarter")
}

func TestDigestAndRepoIndexGenerate(t *testing.T) {
	tgz := packageFixture(t)
	digest, err := ChartDigest(tgz)
	require.NoError(t, err)
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, digest)

	dir := filepath.Dir(tgz)
	out, err := GenerateRepoIndex(dir, RepoIndexGenerateOptions{BaseURL: "https://charts.example/", JSON: true})
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "index.yaml"))
	assert.FileExists(t, filepath.Join(dir, "index.json"))
	var idx struct {
		Entries map[string][]struct {
			Digest string   `json:"digest"`
			URLs   []string `json:"urls"`
		} `json:"entries"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &idx))
	require.Contains(t, idx.Entries, "testchart")
	assert.Equal(t, digest, "sha256:"+idx.Entries["testchart"][0].Digest, "index digest matches ChartDigest")
	assert.Contains(t, idx.Entries["testchart"][0].URLs[0], "https://charts.example/")

	// Merge preserves entries the directory no longer has.
	other := t.TempDir()
	merged, err := GenerateRepoIndex(other, RepoIndexGenerateOptions{Merge: filepath.Join(dir, "index.yaml")})
	require.NoError(t, err)
	assert.Contains(t, merged, `"testchart"`)
}

func TestValuesFromYAML(t *testing.T) {
	out, err := ValuesFromYAML("a: 1\nb:\n  c: [x, z]\n")
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":1,"b":{"c":["x","z"]}}`, out)
	_, err = ValuesFromYAML("a: [unclosed")
	assert.Equal(t, cerrors.CodeValues, cerrors.FromError(err))
}

func TestDependencyListStatuses(t *testing.T) {
	srv := startRepoServer(t)
	dir := writeParentChart(t, srv.URL())

	out, err := DependencyList(dir)
	require.NoError(t, err)
	assert.Contains(t, out, `"status":"missing"`)

	require.NoError(t, DependencyUpdate(dir, DependencyOptions{}))
	out, err = DependencyList(dir)
	require.NoError(t, err)
	assert.Contains(t, out, `"status":"ok"`)

	out, err = DependencyList(testChart)
	require.NoError(t, err)
	assert.Equal(t, "[]", out, "no declared dependencies")
}

func TestSignChartThenVerify(t *testing.T) {
	tgz := packageFixture(t)
	secring := filepath.Join(signingDir, "secring.gpg")
	prov, err := SignChart(tgz, SignOptions{Key: "helm-c-sdk-test", Keyring: secring})
	require.NoError(t, err)
	assert.Equal(t, tgz+".prov", prov)
	assert.FileExists(t, prov)

	out, err := VerifyChart(tgz, "", filepath.Join(signingDir, "pubring.gpg"))
	require.NoError(t, err)
	assert.Contains(t, out, "signed_by")

	_, err = SignChart(tgz, SignOptions{Keyring: secring})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err), "key is required")
	_, err = SignChart(tgz, SignOptions{Key: "helm-c-sdk-test"})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err), "keyring is required")
	_, err = SignChart(tgz, SignOptions{Key: "nobody", Keyring: secring})
	assert.Equal(t, cerrors.CodeChartInvalid, cerrors.FromError(err), "unknown key")

	// package --sign in one step
	signed, err := PackageChart(testChart, PackageOptions{
		Destination: t.TempDir(), Sign: true, Key: "helm-c-sdk-test", Keyring: secring,
	})
	require.NoError(t, err)
	assert.FileExists(t, signed+".prov")
}

func TestRegistryTagsAndResolve(t *testing.T) {
	srv := startOCIRegistry(t)
	client := newTestRegistryClient(t)
	require.NoError(t, RegistryLogin(client, srv.RegistryURL, srv.TestUsername, srv.TestPassword,
		LoginOptions{Insecure: true, PlainHTTP: true}))
	_, err := PushChart(client, packageFixture(t), fmt.Sprintf("oci://%s/tags-test", srv.RegistryURL), PushOptions{PlainHTTP: true})
	require.NoError(t, err)

	ref := fmt.Sprintf("oci://%s/tags-test/testchart", srv.RegistryURL)
	tags, err := RegistryTags(client, ref)
	require.NoError(t, err)
	assert.Equal(t, `["0.1.0"]`, tags)

	resolved, err := RegistryResolve(client, ref+":0.1.0")
	require.NoError(t, err)
	assert.Contains(t, resolved, `"digest":"sha256:`)

	_, err = RegistryTags(client, fmt.Sprintf("oci://%s/nope/none", srv.RegistryURL))
	assert.Equal(t, cerrors.CodeRegistry, cerrors.FromError(err))
}

func TestSetConfigRegistryClient(t *testing.T) {
	cfg := newMemoryConfig(t)
	client := newTestRegistryClient(t)
	require.NoError(t, SetConfigRegistryClient(cfg, client))
	assert.NotNil(t, cfg.Cfg.RegistryClient)
	require.NoError(t, SetConfigRegistryClient(cfg, nil))
	assert.Nil(t, cfg.Cfg.RegistryClient)
	assert.Equal(t, cerrors.CodeWrongHandleType, cerrors.FromError(SetConfigRegistryClient(cfg, "not a client")))
}

func TestGetAndTestRelease(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := InstallRelease(context.Background(), cfg, loadFixtureChart(t), "", "get-rel", `{"replicaCount":7}`, InstallOptions{})
	require.NoError(t, err)

	out, err := GetRelease(cfg, "get-rel", GetOptions{})
	require.NoError(t, err)
	var got struct {
		Summary summaryJSON    `json:"summary"`
		Hooks   []any          `json:"hooks"`
		Config  map[string]any `json:"config"`
		Info    map[string]any `json:"info"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, "get-rel", got.Summary.Name)
	assert.Equal(t, float64(7), got.Config["replicaCount"])
	assert.Equal(t, "deployed", got.Info["status"])

	out, err = TestRelease(cfg, "get-rel", TestOptions{Logs: true})
	require.NoError(t, err)
	assert.Contains(t, out, `"release":`)
	assert.Contains(t, out, `"logs":""`, "fixture chart has no test hooks")

	_, err = GetRelease(cfg, "absent", GetOptions{})
	assert.Equal(t, cerrors.CodeNotFound, cerrors.FromError(err))
}

func TestShowChartLocal(t *testing.T) {
	out, err := ShowChart(nil, testChart, ShowOptions{Format: "chart"})
	require.NoError(t, err)
	assert.Contains(t, out, "name: testchart")

	out, err = ShowChart(nil, testChart, ShowOptions{Format: "values"})
	require.NoError(t, err)
	assert.Contains(t, out, "replicaCount")

	_, err = ShowChart(nil, testChart, ShowOptions{Format: "poster"})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
}

func TestLintWithOptionsAndRenderKubeVersion(t *testing.T) {
	out, err := LintChartWithOptions(testChart, "", LintOptions{Strict: true, KubeVersion: "v1.30.0", Namespace: "ns"})
	require.NoError(t, err)
	assert.Contains(t, out, "total_charts_linted")
	_, err = LintChartWithOptions(testChart, "", LintOptions{KubeVersion: "garbage"})
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))

	dir := filepath.Join(t.TempDir(), "caps")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: caps\nversion: 0.1.0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "templates", "c.yaml"),
		[]byte("kube: {{ .Capabilities.KubeVersion.Version }}\nhas: {{ .Capabilities.APIVersions.Has \"custom.io/v9\" }}\n"), 0o644))
	c, err := LoadChart(dir)
	require.NoError(t, err)
	opts, err := ParseRenderOptions(`{"kube_version":"v1.99.0","api_versions":["custom.io/v9"]}`)
	require.NoError(t, err)
	out, err = RenderChart(c, "", opts)
	require.NoError(t, err)
	assert.Contains(t, out, "kube: v1.99.0")
	assert.Contains(t, out, "has: true")
}

func TestRenderStrictFailsOnMissingValue(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "strict")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "templates"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte("apiVersion: v2\nname: strict\nversion: 0.1.0\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "templates", "c.yaml"), []byte("v: {{ .Values.missing }}\n"), 0o644))
	c, err := LoadChart(dir)
	require.NoError(t, err)

	lax, err := ParseRenderOptions("")
	require.NoError(t, err)
	_, err = RenderChart(c, "", lax)
	require.NoError(t, err)

	strict, err := ParseRenderOptions(`{"strict":true}`)
	require.NoError(t, err)
	_, err = RenderChart(c, "", strict)
	assert.Equal(t, cerrors.CodeRender, cerrors.FromError(err))
}

func TestRemoteChartRefWithoutRepoIsChartLoadError(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := InstallRelease(context.Background(), cfg, nil, "no-such-local-or-remote", "x", "", InstallOptions{})
	assert.Equal(t, cerrors.CodeChartLoad, cerrors.FromError(err))
}
