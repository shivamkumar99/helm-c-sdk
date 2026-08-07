package wrapper

import (
	"os"

	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/getter"
	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// RepoIndexOptions is the JSON options contract of helm_repo_index_download.
// Keys are ABI: additive only.
type RepoIndexOptions struct {
	Username              string `json:"username"`
	Password              string `json:"password"`
	InsecureSkipTLSVerify bool   `json:"insecure_skip_tls_verify"`
}

// ParseRepoIndexOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseRepoIndexOptions(optsJSON string) (RepoIndexOptions, error) {
	return decodeOptions[RepoIndexOptions](optsJSON, "repo index")
}

// DownloadRepoIndex fetches a chart repository's index.yaml and returns it as
// JSON ({"apiVersion", "entries": {chart: [versions...]}, ...}). The cache
// files go to a private temp dir, never the user's helm cache.
func DownloadRepoIndex(repoURL string, opts RepoIndexOptions) (string, error) {
	entry := &repo.Entry{
		Name:                  "helm-c-index",
		URL:                   repoURL,
		Username:              opts.Username,
		Password:              opts.Password,
		InsecureSkipTLSVerify: opts.InsecureSkipTLSVerify,
	}
	r, err := repo.NewChartRepository(entry, getter.All(cli.New()))
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRepo, err)
	}

	cacheDir, err := os.MkdirTemp("", "helm-c-repo-*")
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeIO, err)
	}
	defer os.RemoveAll(cacheDir)
	r.CachePath = cacheDir

	idxPath, err := r.DownloadIndexFile()
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRepo, err)
	}
	idx, err := repo.LoadIndexFile(idxPath)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRepo, err)
	}
	return marshalJSON(idx)
}
