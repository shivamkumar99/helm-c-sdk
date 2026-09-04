package wrapper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/postrenderer"
	"helm.sh/helm/v4/pkg/registry"
	"helm.sh/helm/v4/pkg/release"
	"helm.sh/helm/v4/pkg/storage/driver"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// ---------------------------------------------------------------------------
// Shared plumbing
// ---------------------------------------------------------------------------

// withConfig recovers the *Config behind a registry object and runs the
// action with it — the prologue every release action shares.
func withConfig[T any](cfgObj any, run func(cfg *Config) (T, error)) (T, error) {
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		var zero T
		return zero, err
	}
	return run(cfg)
}

// The SDK reports an unreachable API server with this fixed phrase (every
// action starts with KubeClient.IsReachable, kube/client.go). There is no
// sentinel error to test for, so the message is the only handle on it;
// matched case-insensitively since the CLI has capitalised it differently
// across releases.
const kubeUnreachablePhrase = "kubernetes cluster unreachable"

// wrapActionError classifies an action failure: cancellation, not-found and
// an unreachable cluster keep their dedicated codes through any wrapping;
// everything else gets the action's fallback code.
func wrapActionError(err error, fallback cerrors.Code) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return cerrors.WithCode(cerrors.CodeCancelled, err)
	case errors.Is(err, driver.ErrReleaseNotFound):
		return cerrors.WithCode(cerrors.CodeNotFound, err)
	case strings.Contains(strings.ToLower(err.Error()), kubeUnreachablePhrase):
		return cerrors.WithCode(cerrors.CodeKube, err)
	}
	return cerrors.WithCode(fallback, err)
}

// checkNotCancelled guards the SDK entry: helm v4.2.3's RunWithContext races
// (detached install goroutine vs failRelease, both SetStatus) when handed an
// already-cancelled context, and the SDK is off-limits to fix — so a
// cancelled context never reaches it.
func checkNotCancelled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return cerrors.WithCode(cerrors.CodeCancelled, err)
	}
	return nil
}

// actionPrologue is the shared entry sequence of install/upgrade: cancellation
// guard, config recovery, and values decoding.
func actionPrologue(ctx context.Context, cfgObj any, valuesJSON string) (*Config, map[string]any, error) {
	if err := checkNotCancelled(ctx); err != nil {
		return nil, nil, err
	}
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		return nil, nil, err
	}
	vals, err := unmarshalValues(valuesJSON)
	if err != nil {
		return nil, nil, err
	}
	return cfg, vals, nil
}

// parseDryRun accepts exactly what the helm CLI's --dry-run accepts: "",
// "none", "client", "server", and the legacy booleans ("true" = client,
// "false" = none). The SDK itself never validates the value — any other
// string silently becomes a "template" run against fakes that reports the
// release as deployed — so it is rejected here.
func parseDryRun(s string) (action.DryRunStrategy, error) {
	switch s {
	case "", string(action.DryRunNone):
		return action.DryRunNone, nil
	case string(action.DryRunClient):
		return action.DryRunClient, nil
	case string(action.DryRunServer):
		return action.DryRunServer, nil
	}
	if b, err := strconv.ParseBool(s); err == nil {
		if b {
			return action.DryRunClient, nil
		}
		return action.DryRunNone, nil
	}
	return action.DryRunNone, cerrors.New(cerrors.CodeInvalidArg,
		fmt.Sprintf(`invalid dry_run value %q: must be "none", "client" or "server"`, s))
}

// runTargets points at the timing/strategy fields every mutating action
// shares (install, upgrade, uninstall, rollback); apply copies the option
// values in with the documented defaulting rules. dryRun may be nil for
// actions whose dry run is a plain bool.
type runTargets struct {
	timeout *time.Duration
	wait    *kube.WaitStrategy
	dryRun  *action.DryRunStrategy
}

func (t runTargets) apply(timeoutSeconds int, wait, dryRun string) error {
	if timeoutSeconds > 0 {
		*t.timeout = time.Duration(timeoutSeconds) * time.Second
	}
	*t.wait = defaultWaitStrategy(wait)
	if t.dryRun != nil {
		strategy, err := parseDryRun(dryRun)
		if err != nil {
			return err
		}
		*t.dryRun = strategy
	}
	return nil
}

// defaultWaitStrategy maps an empty opts "wait" to hookOnly — the helm CLI
// default. The SDK errors on an unset strategy against a real cluster
// ("wait strategy not set"), so leaving it empty is never valid.
func defaultWaitStrategy(wait string) kube.WaitStrategy {
	if wait == "" {
		return kube.HookOnlyStrategy
	}
	return kube.WaitStrategy(wait)
}

// finishRelease is the shared exit sequence of every action that yields one
// release: error mapping and summary marshalling.
func finishRelease(rel release.Releaser, err error) (string, error) {
	if err != nil {
		return "", wrapActionError(err, cerrors.CodeRelease)
	}
	s, err := summarizeRelease(rel, true)
	if err != nil {
		return "", err
	}
	return marshalJSON(s)
}

// summariesJSON is the shared exit sequence of actions yielding a list.
func summariesJSON(rels []release.Releaser, err error) (string, error) {
	if err != nil {
		return "", wrapActionError(err, cerrors.CodeRelease)
	}
	summaries, err := summarizeReleases(rels)
	if err != nil {
		return "", err
	}
	return marshalJSON(summaries)
}

// ---------------------------------------------------------------------------
// Chart resolution for install / upgrade
// ---------------------------------------------------------------------------

// ChartRefOptions are the opts_json keys that steer resolution of a chart
// reference (the chart_ref path of helm_install/helm_upgrade). They mirror
// the SDK's ChartPathOptions and are shared by install and upgrade.
type ChartRefOptions struct {
	ChartRepoURL          string `json:"chart_repo_url"`
	ChartVersion          string `json:"chart_version"`
	PlainHTTP             bool   `json:"plain_http"`
	Username              string `json:"username"`
	Password              string `json:"password"`
	CaFile                string `json:"ca_file"`
	CertFile              string `json:"cert_file"`
	KeyFile               string `json:"key_file"`
	InsecureSkipTLSVerify bool   `json:"insecure_skip_tls_verify"`
	Verify                bool   `json:"verify"`
	Keyring               string `json:"keyring"`
	PassCredentialsAll    bool   `json:"pass_credentials_all"`
	Devel                 bool   `json:"devel"`
	// DependencyUpdate fetches missing declared dependencies before the
	// action, like `--dependency-update`. Only possible for a chart_ref that
	// names a local directory; a loaded handle has no directory to update.
	DependencyUpdate bool `json:"dependency_update"`
}

// resolvedChart is a chart ready for an action plus, when it came from a
// local directory, that directory (the only place dependencies can be
// updated into).
type resolvedChart struct {
	chart *chart.Chart
	dir   string
}

// chartForAction returns the chart to operate on: EITHER a loaded chart
// handle object OR a chart reference — never both. A reference is a local
// directory / .tgz, or a remote chart (repo name + chart_repo_url, or an
// oci:// URL) which is pulled through this library's own silent, private
// download path rather than the SDK's LocateChart (that one reads the
// host user's repository config and writes progress to stdout).
func chartForAction(cfg *Config, chartObj any, chartRef string, opts ChartRefOptions) (resolvedChart, error) {
	if chartObj != nil && chartRef != "" {
		return resolvedChart{}, cerrors.New(cerrors.CodeInvalidArg,
			"pass either a chart handle or a chart_ref, not both")
	}
	if chartObj != nil {
		c, err := AsChart(chartObj)
		return resolvedChart{chart: c}, err
	}
	if chartRef == "" {
		return resolvedChart{}, cerrors.New(cerrors.CodeInvalidArg,
			"either a chart handle or a chart_ref is required")
	}
	if info, err := os.Stat(chartRef); err == nil {
		c, err := LoadChart(chartRef)
		if err != nil {
			return resolvedChart{}, err
		}
		dir := ""
		if info.IsDir() {
			dir = chartRef
		}
		return resolvedChart{chart: c, dir: dir}, nil
	}
	return pullChartRef(cfg, chartRef, opts)
}

// pullChartRef downloads a remote chart reference into a private scratch
// directory, loads it, and removes the scratch directory again.
func pullChartRef(cfg *Config, chartRef string, opts ChartRefOptions) (resolvedChart, error) {
	if opts.ChartRepoURL == "" && !registry.IsOCI(chartRef) {
		return resolvedChart{}, cerrors.New(cerrors.CodeChartLoad,
			fmt.Sprintf("chart_ref %q is not a local path; remote charts need chart_repo_url or an oci:// reference", chartRef))
	}
	dir, err := privateTempDir("chart")
	if err != nil {
		return resolvedChart{}, err
	}
	defer removeBestEffort(dir)

	var clientObj any
	if cfg.Cfg.RegistryClient != nil {
		clientObj = cfg.Cfg.RegistryClient
	}
	if _, err := PullChart(clientObj, chartRef, PullOptions{
		DestDir:               dir,
		Version:               opts.ChartVersion,
		RepoURL:               opts.ChartRepoURL,
		PlainHTTP:             opts.PlainHTTP,
		InsecureSkipTLSVerify: opts.InsecureSkipTLSVerify,
		Username:              opts.Username,
		Password:              opts.Password,
		CaFile:                opts.CaFile,
		CertFile:              opts.CertFile,
		KeyFile:               opts.KeyFile,
		Verify:                opts.Verify,
		Keyring:               opts.Keyring,
		PassCredentialsAll:    opts.PassCredentialsAll,
		Devel:                 opts.Devel,
	}); err != nil {
		return resolvedChart{}, cerrors.WithCode(cerrors.CodeChartLoad, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return resolvedChart{}, cerrors.New(cerrors.CodeChartLoad,
			fmt.Sprintf("pulling %q produced no archive", chartRef))
	}
	c, err := LoadChart(dir + string(os.PathSeparator) + entries[0].Name())
	return resolvedChart{chart: c}, err
}

// ensureDependencies mirrors the helm CLI's pre-flight: every dependency
// declared in Chart.yaml must be present under charts/, otherwise the
// action stops — unless the caller asked for a dependency update, which is
// only possible when the chart came from a local directory. The SDK's
// action layer performs no such check, so without this a chart missing a
// subchart would install silently without it.
func ensureDependencies(rc resolvedChart, update bool, opts ChartRefOptions) (*chart.Chart, error) {
	ac, err := chartAccessor(rc.chart)
	if err != nil {
		return nil, err
	}
	req := ac.MetaDependencies()
	if len(req) == 0 {
		return rc.chart, nil
	}
	if err := action.CheckDependencies(rc.chart, req); err == nil {
		return rc.chart, nil
	} else if !update || rc.dir == "" {
		return nil, cerrors.WithCode(cerrors.CodeChartInvalid, fmt.Errorf(
			"chart dependencies are missing; run dependency update/build first: %w", err))
	}
	if err := DependencyUpdate(rc.dir, DependencyOptions{
		Keyring: opts.Keyring, Verify: opts.Verify, PlainHTTP: opts.PlainHTTP,
	}); err != nil {
		return nil, err
	}
	return LoadChart(rc.dir)
}

// ---------------------------------------------------------------------------
// Rendering options shared by install and upgrade
// ---------------------------------------------------------------------------

// renderTweaks are the install/upgrade options that shape rendering and
// application rather than chart selection.
type renderTweaks struct {
	KubeVersion        string   `json:"kube_version"`
	APIVersions        []string `json:"api_versions"`
	PostRenderer       string   `json:"post_renderer"`
	PostRendererArgs   []string `json:"post_renderer_args"`
	PostRenderStrategy string   `json:"post_render_strategy"` // "", "combined", "separate", "nohooks"
	EnableDNS          bool     `json:"enable_dns"`
}

// postRendererFor builds the SDK plugin post-renderer named in opts.
// Post-renderers are helm plugins installed by the host user, so this is
// the one deliberate use of the user's helm plugin directory.
func postRendererFor(name string, args []string) (postrenderer.PostRenderer, error) {
	if name == "" {
		return nil, nil
	}
	pr, err := postrenderer.NewPostRendererPlugin(cli.New(), name, args...)
	if err != nil {
		return nil, cerrors.WithCode(cerrors.CodeInvalidArg, err)
	}
	return pr, nil
}

func parseKubeVersion(s string) (*common.KubeVersion, error) {
	if s == "" {
		return nil, nil
	}
	kv, err := common.ParseKubeVersion(s)
	if err != nil {
		return nil, cerrors.WithCode(cerrors.CodeInvalidArg, err)
	}
	return kv, nil
}

func parsePostRenderStrategy(s string) (action.PostRenderStrategy, error) {
	switch s {
	case "":
		return action.PostRenderStrategyCombined, nil
	case string(action.PostRenderStrategyCombined), string(action.PostRenderStrategySeparate),
		string(action.PostRenderStrategyNoHooks):
		return action.PostRenderStrategy(s), nil
	}
	return "", cerrors.New(cerrors.CodeInvalidArg,
		fmt.Sprintf(`invalid post_render_strategy %q: must be "combined", "separate" or "nohooks"`, s))
}
