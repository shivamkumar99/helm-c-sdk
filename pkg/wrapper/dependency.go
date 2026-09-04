package wrapper

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/downloader"
	"helm.sh/helm/v4/pkg/getter"
	repo "helm.sh/helm/v4/pkg/repo/v1"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// DependencyOptions is the JSON options contract of
// helm_dependency_update/build. Keys are ABI: additive only.
type DependencyOptions struct {
	SkipRefresh bool   `json:"skip_refresh"` // don't re-download repo indexes first
	Keyring     string `json:"keyring"`
	Verify      bool   `json:"verify"`      // legacy bool: true = "always"
	VerifyMode  string `json:"verify_mode"` // "never", "always", "if_possible", "later"
	PlainHTTP   bool   `json:"plain_http"`  // for oci:// dependencies
	Debug       bool   `json:"debug"`       // SDK debug output through the log handler
	Username    string `json:"username"`    // basic auth for HTTP repositories
	Password    string `json:"password"`    //
	CertFile    string `json:"cert_file"`   // client TLS for HTTP repositories
	KeyFile     string `json:"key_file"`    //
	CaFile      string `json:"ca_file"`     //
	Insecure    bool   `json:"insecure_skip_tls_verify"`
}

// ParseDependencyOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseDependencyOptions(optsJSON string) (DependencyOptions, error) {
	return decodeOptions[DependencyOptions](optsJSON, "dependency")
}

// writeRepoConfig registers every HTTP(S) repository the chart's declared
// dependencies point at in a private repositories.yaml — the Manager refuses
// URLs it has no repo entry for ("no repository definition"), and we never
// touch the user's own helm config. Credentials/TLS from opts apply to every
// registered repository.
func writeRepoConfig(chartDir, path string, opts DependencyOptions) error {
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
		repoFile.Add(&repo.Entry{
			Name:                  fmt.Sprintf("helm-c-dep-%d", i),
			URL:                   url,
			Username:              opts.Username,
			Password:              opts.Password,
			CertFile:              opts.CertFile,
			KeyFile:               opts.KeyFile,
			CAFile:                opts.CaFile,
			InsecureSkipTLSVerify: opts.Insecure,
		})
	}
	if err := repoFile.WriteFile(path, 0o600); err != nil {
		return cerrors.WithCode(cerrors.CodeIO, err)
	}
	return nil
}

// dependencyManager builds a downloader.Manager over a private cache dir so
// the user's helm config/cache are never touched. Callers must remove the
// returned cleanup dir.
func dependencyManager(chartDir string, opts DependencyOptions) (*downloader.Manager, string, error) {
	verify, err := verificationStrategy(opts.VerifyMode)
	if err != nil {
		return nil, "", err
	}
	if opts.Verify && opts.VerifyMode == "" {
		verify = downloader.VerifyAlways
	}

	cacheDir, err := privateTempDir("deps")
	if err != nil {
		return nil, "", err
	}
	settings := settingsInDir(cacheDir)
	if err := writeRepoConfig(chartDir, settings.RepositoryConfig, opts); err != nil {
		removeBestEffort(cacheDir)
		return nil, "", err
	}
	client, err := defaultClientFor(nil, true, opts.PlainHTTP)
	if err != nil {
		removeBestEffort(cacheDir)
		return nil, "", err
	}

	m := &downloader.Manager{
		Out:              LogWriter(slog.LevelInfo),
		ChartPath:        chartDir,
		Getters:          getter.All(settings),
		RegistryClient:   client,
		RepositoryConfig: settings.RepositoryConfig,
		RepositoryCache:  settings.RepositoryCache,
		ContentCache:     settings.ContentCache,
		SkipUpdate:       opts.SkipRefresh,
		Keyring:          opts.Keyring,
		Verify:           verify,
		Debug:            opts.Debug,
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
	defer removeBestEffort(cacheDir)
	return cerrors.WithCode(cerrors.CodeRepo, m.Update())
}

// DependencyBuild rebuilds charts/ from Chart.lock (like
// `helm dependency build`).
func DependencyBuild(chartDir string, opts DependencyOptions) error {
	m, cacheDir, err := dependencyManager(chartDir, opts)
	if err != nil {
		return err
	}
	defer removeBestEffort(cacheDir)
	return cerrors.WithCode(cerrors.CodeRepo, m.Build())
}

// dependencyEntry is the stable JSON shape of one `helm dependency list`
// row. Fields are additive-only (ABI).
type dependencyEntry struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Repository string `json:"repository"`
	Status     string `json:"status"`
}

// DependencyList reports each declared dependency of the chart at chartDir
// with its status — the same vocabulary as `helm dependency list`: "ok",
// "missing", "unpacked", "wrong version", "invalid version", "corrupt",
// "misnamed", "too many matches". Returns a JSON array.
func DependencyList(chartDir string) (string, error) {
	c, err := LoadChart(chartDir)
	if err != nil {
		return "", err
	}
	out := make([]dependencyEntry, 0, len(c.Metadata.Dependencies))
	for _, dep := range c.Metadata.Dependencies {
		out = append(out, dependencyEntry{
			Name:       dep.Name,
			Version:    dep.Version,
			Repository: dep.Repository,
			Status:     dependencyStatus(chartDir, dep, c),
		})
	}
	return marshalJSON(out)
}

// dependencyStatus mirrors the SDK's unexported action.Dependency status
// logic (pkg/action/dependency.go at the pinned release), which is only
// reachable there through a text table.
func dependencyStatus(chartDir string, dep *chart.Dependency, parent *chart.Chart) string {
	if s := packagedStatus(chartDir, dep); s != "" {
		return s
	}
	return unpackedStatus(dep, parent)
}

// packagedStatus reports on the packaged copy of dep under charts/, or ""
// when no single archive settles the question.
func packagedStatus(chartDir string, dep *chart.Dependency) string {
	archives, err := filepath.Glob(filepath.Join(chartDir, "charts", dep.Name+"-*.tgz"))
	if err != nil {
		return "bad pattern"
	}
	if len(archives) > 1 {
		archives = versionedArchives(archives, dep.Name)
		if len(archives) > 1 {
			return "too many matches"
		}
	}
	if len(archives) == 1 {
		return archiveStatus(archives[0], dep)
	}
	return ""
}

// versionedArchives keeps the archives whose name is <dep>-<strict semver>.tgz.
func versionedArchives(archives []string, name string) []string {
	var found []string
	for _, arc := range archives {
		stem := strings.TrimSuffix(filepath.Base(arc), ".tgz")
		if _, err := semver.StrictNewVersion(strings.TrimPrefix(stem, name+"-")); err == nil {
			found = append(found, arc)
		}
	}
	return found
}

// unpackedStatus reports on dep as a subchart directory loaded with parent.
func unpackedStatus(dep *chart.Dependency, parent *chart.Chart) string {
	var sub *chart.Chart
	for _, item := range parent.Dependencies() {
		if item.Name() == dep.Name {
			sub = item
		}
	}
	if sub == nil {
		return "missing"
	}
	if s := versionStatus(sub.Metadata.Version, dep.Version); s != "" {
		return s
	}
	return "unpacked"
}

func archiveStatus(archive string, dep *chart.Dependency) string {
	if _, err := os.Stat(archive); err != nil {
		return ""
	}
	c, err := LoadChart(archive)
	if err != nil {
		return "corrupt"
	}
	if c.Name() != dep.Name {
		return "misnamed"
	}
	if s := versionStatus(c.Metadata.Version, dep.Version); s != "" {
		return s
	}
	return "ok"
}

// versionStatus is "" when have satisfies the want constraint.
func versionStatus(have, want string) string {
	if have == want {
		return ""
	}
	constraint, err := semver.NewConstraint(want)
	if err != nil {
		return "invalid version"
	}
	v, err := semver.NewVersion(have)
	if err != nil {
		return "invalid version"
	}
	if !constraint.Check(v) {
		return "wrong version"
	}
	return ""
}
