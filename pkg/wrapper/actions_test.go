package wrapper

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// newMemoryConfig mirrors the SDK's own action test fixture: in-memory
// release storage + a fake kube client, so the full action lifecycle runs
// without a cluster.
func newMemoryConfig(t *testing.T) *Config {
	t.Helper()
	cfg := action.NewConfiguration(action.ConfigurationSetLogger(slog.DiscardHandler))
	cfg.Releases = storage.Init(driver.NewMemory())
	cfg.KubeClient = &kubefake.FailingKubeClient{
		PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard},
	}
	cfg.Capabilities = common.DefaultCapabilities
	return &Config{Cfg: cfg, Namespace: "default"}
}

func loadFixtureChart(t *testing.T) any {
	t.Helper()
	c, err := LoadChart(testChart)
	require.NoError(t, err)
	return c
}

type summaryJSON struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Revision     int    `json:"revision"`
	Status       string `json:"status"`
	ChartName    string `json:"chart_name"`
	ChartVersion string `json:"chart_version"`
	Manifest     string `json:"manifest"`
}

func TestReleaseLifecycle(t *testing.T) {
	cfg := newMemoryConfig(t)
	chart := loadFixtureChart(t)
	ctx := context.Background()

	// Install revision 1.
	out, err := InstallRelease(ctx, cfg, chart, "", "life-rel", `{"replicaCount":2}`, InstallOptions{})
	require.NoError(t, err)
	var rel summaryJSON
	require.NoError(t, json.Unmarshal([]byte(out), &rel))
	assert.Equal(t, "life-rel", rel.Name)
	assert.Equal(t, "default", rel.Namespace)
	assert.Equal(t, 1, rel.Revision)
	assert.Equal(t, "deployed", rel.Status)
	assert.Equal(t, "testchart", rel.ChartName)
	assert.Contains(t, rel.Manifest, `replicas: "2"`)

	// Status agrees.
	out, err = StatusRelease(cfg, "life-rel", StatusOptions{})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &rel))
	assert.Equal(t, 1, rel.Revision)

	// Upgrade to revision 2 with new values.
	out, err = UpgradeRelease(ctx, cfg, chart, "", "life-rel", `{"replicaCount":5}`, UpgradeOptions{})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &rel))
	assert.Equal(t, 2, rel.Revision)
	assert.Contains(t, rel.Manifest, `replicas: "5"`)

	// User-supplied values round-trip.
	out, err = GetReleaseValues(cfg, "life-rel", GetValuesOptions{})
	require.NoError(t, err)
	assert.JSONEq(t, `{"replicaCount":5}`, out)

	// History shows both revisions.
	out, err = HistoryRelease(cfg, "life-rel", HistoryOptions{})
	require.NoError(t, err)
	var hist []summaryJSON
	require.NoError(t, json.Unmarshal([]byte(out), &hist))
	assert.Len(t, hist, 2)

	// Rollback to revision 1 creates revision 3.
	require.NoError(t, RollbackRelease(cfg, "life-rel", RollbackOptions{Version: 1}))
	out, err = StatusRelease(cfg, "life-rel", StatusOptions{})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &rel))
	assert.Equal(t, 3, rel.Revision)
	assert.Contains(t, rel.Manifest, `replicas: "2"`, "rollback restored revision-1 values")

	// List shows the deployed release.
	out, err = ListReleases(cfg, ListOptions{})
	require.NoError(t, err)
	var list []summaryJSON
	require.NoError(t, json.Unmarshal([]byte(out), &list))
	require.Len(t, list, 1)
	assert.Equal(t, "life-rel", list[0].Name)
	assert.Empty(t, list[0].Manifest, "list summaries exclude manifests")

	// Uninstall.
	out, err = UninstallRelease(cfg, "life-rel", UninstallOptions{})
	require.NoError(t, err)
	var uninst struct {
		Release summaryJSON `json:"release"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &uninst))
	assert.Equal(t, "uninstalled", uninst.Release.Status)

	// Gone from the default list.
	out, err = ListReleases(cfg, ListOptions{})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &list))
	assert.Empty(t, list)
}

func TestInstallCancelledContext(t *testing.T) {
	cfg := newMemoryConfig(t)
	chart := loadFixtureChart(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := InstallRelease(ctx, cfg, chart, "", "cancelled-rel", "", InstallOptions{})
	require.Error(t, err)
	assert.Equal(t, cerrors.CodeCancelled, cerrors.FromError(err),
		"cancellation must surface as HELM_ERR_CANCELLED, got: %v", err)
}

func TestStatusNotFound(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := StatusRelease(cfg, "no-such-release", StatusOptions{})
	assert.Equal(t, cerrors.CodeNotFound, cerrors.FromError(err))
}

func TestUninstallNotFound(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := UninstallRelease(cfg, "no-such-release", UninstallOptions{})
	assert.Equal(t, cerrors.CodeNotFound, cerrors.FromError(err))
}

func TestInstallDryRunPersistsNothing(t *testing.T) {
	cfg := newMemoryConfig(t)
	chart := loadFixtureChart(t)

	out, err := InstallRelease(context.Background(), cfg, chart, "", "dry-rel", "",
		InstallOptions{DryRun: "client"})
	require.NoError(t, err)
	assert.Contains(t, out, `"status":"pending-install"`)

	list, err := ListReleases(cfg, ListOptions{All: true})
	require.NoError(t, err)
	assert.Equal(t, "[]", list, "a dry-run install must not be persisted")
}

func TestInstallInvalidReleaseName(t *testing.T) {
	cfg := newMemoryConfig(t)
	chart := loadFixtureChart(t)

	_, err := InstallRelease(context.Background(), cfg, chart, "", "Invalid_NAME!", "", InstallOptions{})
	require.Error(t, err)
	assert.Equal(t, cerrors.CodeRelease, cerrors.FromError(err))
}

func TestAsConfigWrongObject(t *testing.T) {
	_, err := AsConfig("nope")
	assert.Equal(t, cerrors.CodeWrongHandleType, cerrors.FromError(err))
}

func TestNewConfigMemoryDriverFromKubeconfig(t *testing.T) {
	// A syntactically valid kubeconfig pointing nowhere: config construction
	// must succeed (the cluster is only contacted by actions).
	cfg, err := NewConfig(ConfigOptions{
		KubeconfigPath: kubeconfigPath,
		Namespace:      "testing",
		StorageDriver:  "memory",
	})
	require.NoError(t, err)
	assert.Equal(t, "testing", cfg.Namespace)

	// Every action — even list — first checks cluster reachability, so an
	// unreachable server fails cleanly rather than returning partial data.
	_, err = ListReleases(cfg, ListOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unreachable")
}

func TestBadKubeconfigFailsAtFirstAction(t *testing.T) {
	// Kubeconfig loading is lazy: config construction succeeds even with a
	// garbage file — the first action that needs the cluster surfaces the
	// failure. Documented in docs/API.md for helm_config_new.
	cfg, err := NewConfig(ConfigOptions{
		KubeconfigPath: filepath.Join(testChart, "Chart.yaml"),
		StorageDriver:  "memory",
	})
	require.NoError(t, err)

	chart := loadFixtureChart(t)
	_, err = InstallRelease(context.Background(), cfg, chart, "", "doomed-rel", "", InstallOptions{})
	require.Error(t, err, "the first cluster-touching action must fail")
	assert.NotEqual(t, cerrors.CodeOK, cerrors.FromError(err))
}
