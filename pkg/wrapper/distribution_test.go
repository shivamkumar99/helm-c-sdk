package wrapper

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"helm.sh/helm/v4/pkg/repo/v1/repotest"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// packageFixture packages testchart into a temp dir and returns the tgz path.
func packageFixture(t *testing.T) string {
	t.Helper()
	dest := t.TempDir()
	tgz, err := PackageChart(testChart, PackageOptions{Destination: dest})
	require.NoError(t, err)
	return tgz
}

// startRepoServer serves the packaged fixture chart over HTTP with an index.
func startRepoServer(t *testing.T) *repotest.Server {
	t.Helper()
	tgz := packageFixture(t)
	srv := repotest.NewTempServer(t, repotest.WithChartSourceGlob(filepath.Dir(tgz)+"/*.tgz"))
	t.Cleanup(srv.Stop)
	require.NoError(t, srv.CreateIndex())
	return srv
}

func TestDownloadRepoIndex(t *testing.T) {
	srv := startRepoServer(t)

	out, err := DownloadRepoIndex(srv.URL(), RepoIndexOptions{})
	require.NoError(t, err)

	var idx struct {
		APIVersion string `json:"apiVersion"`
		Entries    map[string][]struct {
			Version string   `json:"version"`
			URLs    []string `json:"urls"`
		} `json:"entries"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &idx))
	require.Contains(t, idx.Entries, "testchart")
	assert.Equal(t, "0.1.0", idx.Entries["testchart"][0].Version)
}

func TestDownloadRepoIndexBadURL(t *testing.T) {
	_, err := DownloadRepoIndex("http://127.0.0.1:1/nope", RepoIndexOptions{})
	assert.Equal(t, cerrors.CodeRepo, cerrors.FromError(err))
}

func TestPullFromHTTPRepo(t *testing.T) {
	srv := startRepoServer(t)
	dest := t.TempDir()

	out, err := PullChart(nil, "testchart", PullOptions{
		RepoURL: srv.URL(),
		Version: "0.1.0",
		DestDir: dest,
	})
	require.NoError(t, err)
	assert.Contains(t, out, "output")
	assert.FileExists(t, filepath.Join(dest, "testchart-0.1.0.tgz"))
}

func TestPullMissingChart(t *testing.T) {
	srv := startRepoServer(t)

	_, err := PullChart(nil, "no-such-chart", PullOptions{
		RepoURL: srv.URL(),
		DestDir: t.TempDir(),
	})
	assert.Equal(t, cerrors.CodeRepo, cerrors.FromError(err))
}

// startOCIRegistry boots the SDK's in-process, basic-auth OCI registry and
// waits until it accepts connections.
func startOCIRegistry(t *testing.T) *repotest.OCIServer {
	t.Helper()
	srv, err := repotest.NewOCIServer(t, t.TempDir())
	require.NoError(t, err)
	go func() { _ = srv.ListenAndServe() }()

	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", srv.RegistryURL, 200*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 10*time.Second, 100*time.Millisecond, "OCI test registry never came up")
	return srv
}

func newTestRegistryClient(t *testing.T) any {
	t.Helper()
	credFile := filepath.Join(t.TempDir(), "config.json")
	client, err := NewRegistryClient(RegistryClientOptions{
		PlainHTTP:       true,
		CredentialsFile: credFile,
	})
	require.NoError(t, err)
	return client
}

func TestOCILoginPushPullRoundTrip(t *testing.T) {
	srv := startOCIRegistry(t)
	client := newTestRegistryClient(t)

	// Wrong credentials must fail with a registry error.
	err := RegistryLogin(client, srv.RegistryURL, srv.TestUsername, "wrong-password",
		LoginOptions{Insecure: true, PlainHTTP: true})
	assert.Equal(t, cerrors.CodeRegistry, cerrors.FromError(err))

	// Real credentials succeed.
	require.NoError(t, RegistryLogin(client, srv.RegistryURL, srv.TestUsername,
		srv.TestPassword, LoginOptions{Insecure: true, PlainHTTP: true}))

	// Push the packaged fixture.
	tgz := packageFixture(t)
	remote := fmt.Sprintf("oci://%s/helmc-test", srv.RegistryURL)
	out, err := PushChart(client, tgz, remote, PushOptions{PlainHTTP: true})
	require.NoError(t, err)
	assert.Contains(t, out, "output")

	// Pull it back by OCI reference.
	dest := t.TempDir()
	ref := fmt.Sprintf("oci://%s/helmc-test/testchart", srv.RegistryURL)
	_, err = PullChart(client, ref, PullOptions{
		Version:   "0.1.0",
		DestDir:   dest,
		PlainHTTP: true,
	})
	require.NoError(t, err)
	pulled := filepath.Join(dest, "testchart-0.1.0.tgz")
	require.FileExists(t, pulled)

	// The pulled archive is a loadable chart identical in identity.
	c, err := LoadChart(pulled)
	require.NoError(t, err)
	meta, err := ChartMetadataJSON(c)
	require.NoError(t, err)
	assert.Contains(t, meta, `"name":"testchart"`)

	// Logout removes the stored credentials.
	require.NoError(t, RegistryLogout(client, srv.RegistryURL))
}

func TestPushWithoutAuthFails(t *testing.T) {
	srv := startOCIRegistry(t)
	client := newTestRegistryClient(t) // never logged in

	tgz := packageFixture(t)
	remote := fmt.Sprintf("oci://%s/helmc-unauth", srv.RegistryURL)
	_, err := PushChart(client, tgz, remote, PushOptions{PlainHTTP: true})
	assert.Equal(t, cerrors.CodeRegistry, cerrors.FromError(err))
}

func TestAsRegistryClientWrongObject(t *testing.T) {
	_, err := AsRegistryClient(42)
	assert.Equal(t, cerrors.CodeWrongHandleType, cerrors.FromError(err))
}

func TestPullOptionsUnknownKey(t *testing.T) {
	_, err := ParsePullOptions(`{"bogus":1}`)
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
}

func TestRegistryClientOptionsRoundTrip(t *testing.T) {
	opts, err := ParseRegistryClientOptions(`{"plain_http":true,"credentials_file":"/tmp/c.json"}`)
	require.NoError(t, err)
	assert.True(t, opts.PlainHTTP)
	assert.Equal(t, "/tmp/c.json", opts.CredentialsFile)
}

// Guard: pulls default DestDir to "." — make sure our tests never write into
// the repo by accident (the wrapper itself must honor DestDir).
func TestPullHonorsDestDir(t *testing.T) {
	srv := startRepoServer(t)
	dest := t.TempDir()
	_, err := PullChart(nil, "testchart", PullOptions{RepoURL: srv.URL(), DestDir: dest})
	require.NoError(t, err)

	entries, err := os.ReadDir(dest)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "exactly the pulled archive in DestDir")
}
