package wrapper

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/downloader"
	"helm.sh/helm/v4/pkg/getter"
	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

// DependencyOptions is the JSON options contract of
// helm_dependency_update/build. Keys are ABI: additive only.
type DependencyOptions struct {
	SkipRefresh bool   `json:"skip_refresh"` // don't re-download repo indexes first
	Keyring     string `json:"keyring"`
	Verify      bool   `json:"verify"`
	PlainHTTP   bool   `json:"plain_http"` // for oci:// dependencies
}

// ParseDependencyOptions strictly decodes optsJSON (ADR-0004).
func ParseDependencyOptions(optsJSON string) (DependencyOptions, error) {
	return decodeOptions[DependencyOptions](optsJSON, "dependency")
}

// writeRepoConfig registers every HTTP(S) repository the chart's declared
// dependencies point at in a private repositories.yaml — the Manager refuses
// URLs it has no repo entry for ("no repository definition"), and we never
// touch the user's own helm config.
func writeRepoConfig(chartDir, path string) error {
	c, err := LoadChart(chartDir)
	if err != nil {
		return err
	}

	repoFile := repo.NewFile()
	seen := map[string]bool{}
	for i, dep := range c.Metadata.Dependencies {
		url := dep.Repository
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			continue // oci:// and file:// need no repo entry
		}
		if seen[url] {
			continue
		}
		seen[url] = true
		repoFile.Add(&repo.Entry{Name: fmt.Sprintf("helm-c-dep-%d", i), URL: url})
	}
	if err := repoFile.WriteFile(path, 0o644); err != nil {
		return cerrors.WithCode(cerrors.CodeIO, err)
	}
	return nil
}

// dependencyManager builds a downloader.Manager over a private cache dir so
// the user's helm config/cache are never touched. Callers must remove the
// returned cleanup dir.
func dependencyManager(chartDir string, opts DependencyOptions) (*downloader.Manager, string, error) {
	cacheDir, err := os.MkdirTemp("", "helm-c-deps-*")
	if err != nil {
		return nil, "", cerrors.WithCode(cerrors.CodeIO, err)
	}

	repoConfig := filepath.Join(cacheDir, "repositories.yaml")
	if err := writeRepoConfig(chartDir, repoConfig); err != nil {
		os.RemoveAll(cacheDir)
		return nil, "", err
	}

	client, err := NewRegistryClient(RegistryClientOptions{PlainHTTP: opts.PlainHTTP})
	if err != nil {
		os.RemoveAll(cacheDir)
		return nil, "", err
	}

	m := &downloader.Manager{
		Out:              LogWriter(slog.LevelInfo),
		ChartPath:        chartDir,
		Getters:          getter.All(cli.New()),
		RegistryClient:   client,
		RepositoryConfig: repoConfig,
		RepositoryCache:  filepath.Join(cacheDir, "cache"),
		ContentCache:     filepath.Join(cacheDir, "content"),
		SkipUpdate:       opts.SkipRefresh,
		Keyring:          opts.Keyring,
	}
	if opts.Verify {
		m.Verify = downloader.VerifyAlways
	}
	return m, cacheDir, nil
}

// DependencyUpdate resolves the chart's declared dependencies, downloads them
// into charts/, and writes Chart.lock (like `helm dependency update`).
func DependencyUpdate(chartDir string, opts DependencyOptions) error {
	m, cacheDir, err := dependencyManager(chartDir, opts)
	if err != nil {
		return err
	}
	defer os.RemoveAll(cacheDir)
	return cerrors.WithCode(cerrors.CodeRepo, m.Update())
}

// DependencyBuild rebuilds charts/ from Chart.lock (like
// `helm dependency build`).
func DependencyBuild(chartDir string, opts DependencyOptions) error {
	m, cacheDir, err := dependencyManager(chartDir, opts)
	if err != nil {
		return err
	}
	defer os.RemoveAll(cacheDir)
	return cerrors.WithCode(cerrors.CodeRepo, m.Build())
}
