package wrapper

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

func TestNewConfigInlineKubeconfigContent(t *testing.T) {
	content, err := os.ReadFile("../../testdata/kubeconfig.yaml")
	require.NoError(t, err)

	cfg, err := NewConfig(ConfigOptions{
		KubeconfigContent: string(content),
		Namespace:         "inline-ns",
		StorageDriver:     "memory",
	})
	require.NoError(t, err)
	assert.Equal(t, "inline-ns", cfg.Namespace)

	// The private temp file exists (0600) while the config is alive...
	require.NotEmpty(t, cfg.tempKubeconfig)
	info, err := os.Stat(cfg.tempKubeconfig)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// ...and Close removes it, idempotently.
	path := cfg.tempKubeconfig
	cfg.Close()
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err))
	cfg.Close() // second close is a no-op
}

func TestNewConfigPathAndContentMutuallyExclusive(t *testing.T) {
	_, err := NewConfig(ConfigOptions{
		KubeconfigPath:    "../../testdata/kubeconfig.yaml",
		KubeconfigContent: "apiVersion: v1",
	})
	require.Error(t, err)
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
}

func TestNewConfigFullKubeSurfaceAccepted(t *testing.T) {
	// Every SDK connection option decodes and builds a config (lazy — the
	// cluster is never contacted here).
	opts, err := ParseConfigOptions(`{
		"kubeconfig_path": "../../testdata/kubeconfig.yaml",
		"kube_context": "helmc-test",
		"kube_token": "tok",
		"kube_apiserver": "https://127.0.0.1:1",
		"kube_tls_server_name": "srv",
		"kube_insecure_skip_tls_verify": true,
		"kube_as_user": "alice",
		"kube_as_groups": ["dev", "ops"],
		"burst_limit": 50,
		"qps": 25.5,
		"namespace": "full",
		"storage_driver": "memory"
	}`)
	require.NoError(t, err)
	assert.Equal(t, []string{"dev", "ops"}, opts.KubeAsGroups)

	cfg, err := NewConfig(opts)
	require.NoError(t, err)
	assert.Equal(t, "full", cfg.Namespace)
}

func TestInstallByLocalPathRef(t *testing.T) {
	cfg := newMemoryConfig(t)

	out, err := InstallRelease(context.Background(), cfg, nil, testChart, "ref-rel", "", InstallOptions{})
	require.NoError(t, err)

	var rel summaryJSON
	require.NoError(t, json.Unmarshal([]byte(out), &rel))
	assert.Equal(t, "testchart", rel.ChartName)
	assert.Equal(t, 1, rel.Revision)
}

func TestInstallByRepoRef(t *testing.T) {
	srv := startRepoServer(t)
	cfg := newMemoryConfig(t)

	out, err := InstallRelease(context.Background(), cfg, nil, "testchart", "repo-ref-rel", "",
		InstallOptions{ChartRepoURL: srv.URL(), ChartVersion: "0.1.0"})
	require.NoError(t, err)
	assert.Contains(t, out, `"chart_name":"testchart"`)

	// Upgrade by ref too — the full CLI-style flow without a chart handle.
	out, err = UpgradeRelease(context.Background(), cfg, nil, "testchart", "repo-ref-rel",
		`{"replicaCount":4}`, UpgradeOptions{ChartRepoURL: srv.URL(), ChartVersion: "0.1.0"})
	require.NoError(t, err)
	var rel summaryJSON
	require.NoError(t, json.Unmarshal([]byte(out), &rel))
	assert.Equal(t, 2, rel.Revision)
}

func TestInstallNeitherHandleNorRef(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := InstallRelease(context.Background(), cfg, nil, "", "no-chart-rel", "", InstallOptions{})
	require.Error(t, err)
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
}

func TestInstallByMissingRef(t *testing.T) {
	cfg := newMemoryConfig(t)
	_, err := InstallRelease(context.Background(), cfg, nil, "does/not/exist-anywhere", "bad-ref-rel", "", InstallOptions{})
	require.Error(t, err)
	assert.Equal(t, cerrors.CodeChartLoad, cerrors.FromError(err))
}
