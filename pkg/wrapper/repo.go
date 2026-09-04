package wrapper

import (
	"path/filepath"

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
	CertFile              string `json:"cert_file"`
	KeyFile               string `json:"key_file"`
	CaFile                string `json:"ca_file"`
	PassCredentialsAll    bool   `json:"pass_credentials_all"`
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
		CertFile:              opts.CertFile,
		KeyFile:               opts.KeyFile,
		CAFile:                opts.CaFile,
		PassCredentialsAll:    opts.PassCredentialsAll,
	}
	r, err := repo.NewChartRepository(entry, getter.All(newSettings()))
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRepo, err)
	}

	cacheDir, err := privateTempDir("repo")
	if err != nil {
		return "", err
	}
	defer removeBestEffort(cacheDir)
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

// RepoIndexGenerateOptions is the JSON options contract of
// helm_repo_index_generate. Keys are ABI: additive only.
type RepoIndexGenerateOptions struct {
	BaseURL string `json:"base_url"` // absolute URL prefix for chart entries
	Merge   string `json:"merge"`    // existing index.yaml to merge into the result
	JSON    bool   `json:"json"`     // also write index.json alongside index.yaml
}

// ParseRepoIndexGenerateOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseRepoIndexGenerateOptions(optsJSON string) (RepoIndexGenerateOptions, error) {
	return decodeOptions[RepoIndexGenerateOptions](optsJSON, "repo index generate")
}

// GenerateRepoIndex indexes the packaged charts (*.tgz) in dir into
// dir/index.yaml, like `helm repo index`. With opts.Merge, entries from that
// existing index are preserved for versions the directory no longer holds.
// Returns the generated index as JSON.
func GenerateRepoIndex(dir string, opts RepoIndexGenerateOptions) (string, error) {
	idx, err := repo.IndexDirectory(dir, opts.BaseURL)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRepo, err)
	}
	if opts.Merge != "" {
		existing, err := repo.LoadIndexFile(opts.Merge)
		if err != nil {
			return "", cerrors.WithCode(cerrors.CodeRepo, err)
		}
		idx.Merge(existing)
	}
	idx.SortEntries()
	if err := idx.WriteFile(filepath.Join(dir, "index.yaml"), 0o644); err != nil {
		return "", cerrors.WithCode(cerrors.CodeIO, err)
	}
	if opts.JSON {
		if err := idx.WriteJSONFile(filepath.Join(dir, "index.json"), 0o644); err != nil {
			return "", cerrors.WithCode(cerrors.CodeIO, err)
		}
	}
	return marshalJSON(idx)
}
