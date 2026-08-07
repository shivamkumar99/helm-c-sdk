package wrapper

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

const signingDir = "../../testdata/signing"

// writeParentChart creates a chart directory declaring testchart as a
// dependency served from repoURL.
func writeParentChart(t *testing.T, repoURL string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "parent")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	chartYAML := fmt.Sprintf(`apiVersion: v2
name: parent
version: 0.1.0
dependencies:
  - name: testchart
    version: "0.1.0"
    repository: %q
`, repoURL)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Chart.yaml"), []byte(chartYAML), 0o644))
	return dir
}

func TestDependencyUpdateAndBuild(t *testing.T) {
	srv := startRepoServer(t)
	dir := writeParentChart(t, srv.URL())

	require.NoError(t, DependencyUpdate(dir, DependencyOptions{}))
	assert.FileExists(t, filepath.Join(dir, "charts", "testchart-0.1.0.tgz"))
	assert.FileExists(t, filepath.Join(dir, "Chart.lock"))

	// Wipe charts/ and rebuild strictly from the lock.
	require.NoError(t, os.RemoveAll(filepath.Join(dir, "charts")))
	require.NoError(t, DependencyBuild(dir, DependencyOptions{}))
	assert.FileExists(t, filepath.Join(dir, "charts", "testchart-0.1.0.tgz"))

	// The parent chart now loads with its dependency embedded.
	c, err := LoadChart(dir)
	require.NoError(t, err)
	meta, err := ChartMetadataJSON(c)
	require.NoError(t, err)
	assert.Contains(t, meta, `"name":"parent"`)
}

func TestDependencyUpdateUnreachableRepo(t *testing.T) {
	dir := writeParentChart(t, "http://127.0.0.1:1")
	err := DependencyUpdate(dir, DependencyOptions{})
	require.Error(t, err)
	assert.Equal(t, cerrors.CodeRepo, cerrors.FromError(err))
}

func TestVerifyChartSigned(t *testing.T) {
	out, err := VerifyChart(
		filepath.Join(signingDir, "signtest-0.1.0.tgz"),
		"", // defaults to <path>.prov
		filepath.Join(signingDir, "helm-test-key.pub"),
	)
	require.NoError(t, err)

	var v struct {
		FileName string   `json:"file_name"`
		FileHash string   `json:"file_hash"`
		SignedBy []string `json:"signed_by"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &v))
	assert.Equal(t, "signtest-0.1.0.tgz", v.FileName)
	assert.NotEmpty(t, v.FileHash)
	assert.NotEmpty(t, v.SignedBy)
}

func TestVerifyChartWithoutProvenance(t *testing.T) {
	tgz := packageFixture(t) // our testchart is unsigned: no .prov exists
	_, err := VerifyChart(tgz, "", filepath.Join(signingDir, "helm-test-key.pub"))
	require.Error(t, err)
	assert.Equal(t, cerrors.CodeChartInvalid, cerrors.FromError(err))
}

func TestGetReleaseMetadata(t *testing.T) {
	cfg := newMemoryConfig(t)
	chart := loadFixtureChart(t)

	_, err := InstallRelease(context.Background(), cfg, chart, "", "meta-rel", "", InstallOptions{})
	require.NoError(t, err)

	out, err := GetReleaseMetadata(cfg, "meta-rel", GetMetadataOptions{})
	require.NoError(t, err)
	var meta struct {
		Name    string `json:"name"`
		Chart   string `json:"chart"`
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &meta))
	assert.Equal(t, "meta-rel", meta.Name)
	assert.Equal(t, "testchart", meta.Chart)
	assert.Equal(t, "0.1.0", meta.Version)

	_, err = GetReleaseMetadata(cfg, "absent", GetMetadataOptions{})
	assert.Equal(t, cerrors.CodeNotFound, cerrors.FromError(err))
}
