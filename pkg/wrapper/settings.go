package wrapper

import (
	"os"
	"path/filepath"

	"helm.sh/helm/v4/pkg/cli"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// newSettings is the single place helm-c builds SDK EnvSettings. Every
// caller goes through it so the policy that this library never reads or
// writes the host user's helm repository config and caches is enforced
// once: the repo config, repo cache and content cache all point at a
// private scratch directory that never resolves to the user's helm home.
// Kube connection fields are left for the caller to set.
func newSettings() *cli.EnvSettings {
	s := cli.New()
	private := filepath.Join(os.TempDir(), "helm-c-private")
	s.RepositoryConfig = filepath.Join(private, "repositories.yaml")
	s.RepositoryCache = filepath.Join(private, "repository")
	s.ContentCache = filepath.Join(private, "content")
	return s
}

// privateTempDir creates a scratch directory for one operation. Callers
// remove it with removeBestEffort when the operation completes.
func privateTempDir(prefix string) (string, error) {
	dir, err := os.MkdirTemp("", "helm-c-"+prefix+"-*")
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeIO, err)
	}
	return dir, nil
}

// settingsInDir returns EnvSettings whose repo config and caches live under
// dir, for operations that must leave nothing behind.
func settingsInDir(dir string) *cli.EnvSettings {
	s := newSettings()
	s.RepositoryConfig = filepath.Join(dir, "repositories.yaml")
	s.RepositoryCache = filepath.Join(dir, "repository")
	s.ContentCache = filepath.Join(dir, "content")
	return s
}
