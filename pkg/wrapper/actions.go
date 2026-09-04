package wrapper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
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
	rspb "helm.sh/helm/v4/pkg/release/v1"
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

// ---------------------------------------------------------------------------
// Install
// ---------------------------------------------------------------------------

// InstallOptions is the JSON options contract of helm_install. Keys are ABI:
// additive only.
type InstallOptions struct {
	Namespace         string            `json:"namespace"`
	TimeoutSeconds    int               `json:"timeout_seconds"`
	Wait              string            `json:"wait"`    // "", "watcher", "legacy", "hookOnly"
	DryRun            string            `json:"dry_run"` // "", "none", "client", "server"
	CreateNamespace   bool              `json:"create_namespace"`
	RollbackOnFailure bool              `json:"rollback_on_failure"`
	Description       string            `json:"description"`
	Labels            map[string]string `json:"labels"`

	DisableHooks   bool `json:"disable_hooks"`
	ForceReplace   bool `json:"force_replace"`
	ForceConflicts bool `json:"force_conflicts"`
	// ServerSideApply is tri-state: omitted keeps the SDK/CLI default (true).
	ServerSideApply          *bool `json:"server_side_apply"`
	Replace                  bool  `json:"replace"`
	SkipCRDs                 bool  `json:"skip_crds"`
	WaitForJobs              bool  `json:"wait_for_jobs"`
	TakeOwnership            bool  `json:"take_ownership"`
	SubNotes                 bool  `json:"sub_notes"`
	HideNotes                bool  `json:"hide_notes"`
	HideSecret               bool  `json:"hide_secret"`
	SkipSchemaValidation     bool  `json:"skip_schema_validation"`
	DisableOpenAPIValidation bool  `json:"disable_openapi_validation"`

	renderTweaks
	ChartRefOptions
}

// ParseInstallOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseInstallOptions(optsJSON string) (InstallOptions, error) {
	return decodeOptions[InstallOptions](optsJSON, "install")
}

// InstallRelease installs a chart (loaded handle object, or chartRef when
// chartObj is nil) as name and returns the release summary JSON (with
// manifest). ctx cancels the operation (HELM_ERR_CANCELLED) — note the SDK
// keeps applying in the background after a cancelled call returns.
func InstallRelease(ctx context.Context, cfgObj, chartObj any, chartRef, name, valuesJSON string, opts InstallOptions) (string, error) {
	cfg, vals, err := actionPrologue(ctx, cfgObj, valuesJSON)
	if err != nil {
		return "", err
	}
	dryRun, err := parseDryRun(opts.DryRun)
	if err != nil {
		return "", err
	}

	namespace := opts.Namespace
	if namespace == "" {
		namespace = cfg.Namespace
	}
	// The storage and kube client are bound to a namespace at Init, so an
	// install into another namespace needs a configuration for it; and a
	// client-side dry run mutates its Configuration (swaps in fakes), so it
	// must never see the shared one.
	actionCfg, err := cfg.forNamespace(namespace)
	if err != nil {
		return "", err
	}
	if !interactsWithServer(dryRun) {
		actionCfg = (&Config{Cfg: actionCfg, Namespace: namespace, opts: cfg.opts}).detachedConfiguration()
	}

	inst := action.NewInstall(actionCfg)
	inst.ReleaseName = name
	inst.Namespace = namespace
	inst.DryRunStrategy = dryRun
	if err := (runTargets{&inst.Timeout, &inst.WaitStrategy, nil}).apply(opts.TimeoutSeconds, opts.Wait, ""); err != nil {
		return "", err
	}
	if err := applyInstallOptions(inst, opts); err != nil {
		return "", err
	}

	rc, err := chartForAction(cfg, chartObj, chartRef, opts.ChartRefOptions)
	if err != nil {
		return "", err
	}
	c, err := ensureDependencies(rc, opts.DependencyUpdate, opts.ChartRefOptions)
	if err != nil {
		return "", err
	}

	rel, err := inst.RunWithContext(ctx, c, vals)
	return finishRelease(rel, err)
}

// interactsWithServer mirrors the SDK's own predicate: only "none" and
// "server" talk to the API server; "client" (and, in the SDK, any unknown
// value) takes the template path against fakes.
func interactsWithServer(s action.DryRunStrategy) bool {
	return s == action.DryRunNone || s == action.DryRunServer
}

func applyInstallOptions(inst *action.Install, opts InstallOptions) error {
	inst.CreateNamespace = opts.CreateNamespace
	inst.RollbackOnFailure = opts.RollbackOnFailure
	inst.Description = opts.Description
	inst.Labels = opts.Labels
	inst.DisableHooks = opts.DisableHooks
	inst.ForceReplace = opts.ForceReplace
	inst.ForceConflicts = opts.ForceConflicts
	if opts.ServerSideApply != nil {
		inst.ServerSideApply = *opts.ServerSideApply
	}
	inst.Replace = opts.Replace
	inst.SkipCRDs = opts.SkipCRDs
	inst.WaitForJobs = opts.WaitForJobs
	inst.TakeOwnership = opts.TakeOwnership
	inst.SubNotes = opts.SubNotes
	inst.HideNotes = opts.HideNotes
	inst.HideSecret = opts.HideSecret
	inst.SkipSchemaValidation = opts.SkipSchemaValidation
	inst.DisableOpenAPIValidation = opts.DisableOpenAPIValidation
	inst.EnableDNS = opts.EnableDNS
	inst.Devel = opts.Devel
	inst.DependencyUpdate = opts.DependencyUpdate

	kv, err := parseKubeVersion(opts.KubeVersion)
	if err != nil {
		return err
	}
	inst.KubeVersion = kv
	inst.APIVersions = common.VersionSet(opts.APIVersions)

	pr, err := postRendererFor(opts.PostRenderer, opts.PostRendererArgs)
	if err != nil {
		return err
	}
	inst.PostRenderer = pr
	inst.PostRenderStrategy, err = parsePostRenderStrategy(opts.PostRenderStrategy)
	return err
}

// ---------------------------------------------------------------------------
// Upgrade
// ---------------------------------------------------------------------------

// UpgradeOptions is the JSON options contract of helm_upgrade. Keys are ABI:
// additive only.
type UpgradeOptions struct {
	Namespace         string            `json:"namespace"`
	TimeoutSeconds    int               `json:"timeout_seconds"`
	Wait              string            `json:"wait"`
	DryRun            string            `json:"dry_run"`
	MaxHistory        int               `json:"max_history"`
	ResetValues       bool              `json:"reset_values"`
	ReuseValues       bool              `json:"reuse_values"`
	CleanupOnFail     bool              `json:"cleanup_on_fail"`
	RollbackOnFailure bool              `json:"rollback_on_failure"`
	Description       string            `json:"description"`
	Labels            map[string]string `json:"labels"`

	Install                  bool   `json:"install"` // informative only (SDK semantics)
	ResetThenReuseValues     bool   `json:"reset_then_reuse_values"`
	DisableHooks             bool   `json:"disable_hooks"`
	ForceReplace             bool   `json:"force_replace"`
	ForceConflicts           bool   `json:"force_conflicts"`
	ServerSideApply          string `json:"server_side_apply"` // "", "true", "false", "auto"
	SkipCRDs                 bool   `json:"skip_crds"`
	WaitForJobs              bool   `json:"wait_for_jobs"`
	TakeOwnership            bool   `json:"take_ownership"`
	SubNotes                 bool   `json:"sub_notes"`
	HideNotes                bool   `json:"hide_notes"`
	HideSecret               bool   `json:"hide_secret"`
	SkipSchemaValidation     bool   `json:"skip_schema_validation"`
	DisableOpenAPIValidation bool   `json:"disable_openapi_validation"`

	renderTweaks
	ChartRefOptions
}

// ParseUpgradeOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseUpgradeOptions(optsJSON string) (UpgradeOptions, error) {
	return decodeOptions[UpgradeOptions](optsJSON, "upgrade")
}

// UpgradeRelease upgrades name to a chart (loaded handle object, or chartRef
// when chartObj is nil) and returns the release summary JSON (with manifest).
// ctx cancels the operation.
func UpgradeRelease(ctx context.Context, cfgObj, chartObj any, chartRef, name, valuesJSON string, opts UpgradeOptions) (string, error) {
	cfg, vals, err := actionPrologue(ctx, cfgObj, valuesJSON)
	if err != nil {
		return "", err
	}
	// An upgrade always operates in the namespace the release lives in — the
	// SDK reads it from the stored release and ignores Upgrade.Namespace. A
	// different value would be silently ignored, so it is rejected instead.
	if opts.Namespace != "" && opts.Namespace != cfg.Namespace {
		return "", cerrors.New(cerrors.CodeInvalidArg, fmt.Sprintf(
			"upgrade cannot change namespace (config is %q, got %q); create a config for that namespace",
			cfg.Namespace, opts.Namespace))
	}
	switch opts.ServerSideApply {
	case "", "true", "false", "auto":
	default:
		return "", cerrors.New(cerrors.CodeInvalidArg,
			fmt.Sprintf(`invalid server_side_apply %q: must be "true", "false" or "auto"`, opts.ServerSideApply))
	}

	up := action.NewUpgrade(cfg.Cfg)
	up.Namespace = cfg.Namespace
	if err := (runTargets{&up.Timeout, &up.WaitStrategy, &up.DryRunStrategy}).apply(opts.TimeoutSeconds, opts.Wait, opts.DryRun); err != nil {
		return "", err
	}
	if err := applyUpgradeOptions(up, opts); err != nil {
		return "", err
	}

	rc, err := chartForAction(cfg, chartObj, chartRef, opts.ChartRefOptions)
	if err != nil {
		return "", err
	}
	c, err := ensureDependencies(rc, opts.DependencyUpdate, opts.ChartRefOptions)
	if err != nil {
		return "", err
	}

	rel, err := up.RunWithContext(ctx, name, c, vals)
	return finishRelease(rel, err)
}

func applyUpgradeOptions(up *action.Upgrade, opts UpgradeOptions) error {
	up.MaxHistory = opts.MaxHistory
	up.ResetValues = opts.ResetValues
	up.ReuseValues = opts.ReuseValues
	up.ResetThenReuseValues = opts.ResetThenReuseValues
	up.CleanupOnFail = opts.CleanupOnFail
	up.RollbackOnFailure = opts.RollbackOnFailure
	up.Description = opts.Description
	up.Labels = opts.Labels
	up.Install = opts.Install
	up.DisableHooks = opts.DisableHooks
	up.ForceReplace = opts.ForceReplace
	up.ForceConflicts = opts.ForceConflicts
	if opts.ServerSideApply != "" {
		up.ServerSideApply = opts.ServerSideApply // "" keeps the SDK default ("auto")
	}
	up.SkipCRDs = opts.SkipCRDs
	up.WaitForJobs = opts.WaitForJobs
	up.TakeOwnership = opts.TakeOwnership
	up.SubNotes = opts.SubNotes
	up.HideNotes = opts.HideNotes
	up.HideSecret = opts.HideSecret
	up.SkipSchemaValidation = opts.SkipSchemaValidation
	up.DisableOpenAPIValidation = opts.DisableOpenAPIValidation
	up.EnableDNS = opts.EnableDNS
	up.Devel = opts.Devel
	up.DependencyUpdate = opts.DependencyUpdate

	pr, err := postRendererFor(opts.PostRenderer, opts.PostRendererArgs)
	if err != nil {
		return err
	}
	up.PostRenderer = pr
	up.PostRenderStrategy, err = parsePostRenderStrategy(opts.PostRenderStrategy)
	return err
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

// UninstallOptions is the JSON options contract of helm_uninstall. Keys are
// ABI: additive only.
type UninstallOptions struct {
	KeepHistory         bool   `json:"keep_history"`
	TimeoutSeconds      int    `json:"timeout_seconds"`
	DryRun              bool   `json:"dry_run"`
	IgnoreNotFound      bool   `json:"ignore_not_found"`
	Wait                string `json:"wait"`
	Description         string `json:"description"`
	DisableHooks        bool   `json:"disable_hooks"`
	DeletionPropagation string `json:"deletion_propagation"` // "", "background", "foreground", "orphan"
}

// ParseUninstallOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseUninstallOptions(optsJSON string) (UninstallOptions, error) {
	return decodeOptions[UninstallOptions](optsJSON, "uninstall")
}

// UninstallRelease removes name and returns {"info": "...", "release": {...}}.
func UninstallRelease(cfgObj any, name string, opts UninstallOptions) (string, error) {
	return withConfig(cfgObj, func(cfg *Config) (string, error) {
		switch opts.DeletionPropagation {
		case "", "background", "foreground", "orphan":
		default:
			return "", cerrors.New(cerrors.CodeInvalidArg, fmt.Sprintf(
				`invalid deletion_propagation %q: must be "background", "foreground" or "orphan"`, opts.DeletionPropagation))
		}
		u := action.NewUninstall(cfg.Cfg)
		u.KeepHistory = opts.KeepHistory
		u.DryRun = opts.DryRun
		u.IgnoreNotFound = opts.IgnoreNotFound
		u.Description = opts.Description
		u.DisableHooks = opts.DisableHooks
		u.DeletionPropagation = opts.DeletionPropagation
		if err := (runTargets{&u.Timeout, &u.WaitStrategy, nil}).apply(opts.TimeoutSeconds, opts.Wait, ""); err != nil {
			return "", err
		}

		resp, err := u.Run(name)
		if err != nil {
			return "", wrapActionError(err, cerrors.CodeRelease)
		}
		// With IgnoreNotFound set, the SDK reports "there was nothing to
		// remove" as (nil, nil) rather than an empty response, on both the
		// dry-run and the normal path. Dereferencing that is a crash, not an
		// error the caller could catch, so report the no-op instead.
		if resp == nil {
			return marshalJSON(map[string]any{"info": ""})
		}
		out := map[string]any{"info": resp.Info}
		if resp.Release != nil {
			s, err := summarizeRelease(resp.Release, false)
			if err != nil {
				return "", err
			}
			out["release"] = s
		}
		return marshalJSON(out)
	})
}

// ---------------------------------------------------------------------------
// Rollback
// ---------------------------------------------------------------------------

// RollbackOptions is the JSON options contract of helm_rollback. Keys are
// ABI: additive only.
type RollbackOptions struct {
	Version         int    `json:"version"` // 0 = previous revision
	TimeoutSeconds  int    `json:"timeout_seconds"`
	Wait            string `json:"wait"`
	DryRun          string `json:"dry_run"`
	WaitForJobs     bool   `json:"wait_for_jobs"`
	DisableHooks    bool   `json:"disable_hooks"`
	ForceReplace    bool   `json:"force_replace"`
	ForceConflicts  bool   `json:"force_conflicts"`
	ServerSideApply string `json:"server_side_apply"` // "", "true", "false", "auto"
	CleanupOnFail   bool   `json:"cleanup_on_fail"`
	MaxHistory      int    `json:"max_history"`
}

// ParseRollbackOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseRollbackOptions(optsJSON string) (RollbackOptions, error) {
	return decodeOptions[RollbackOptions](optsJSON, "rollback")
}

// RollbackRelease rolls name back to opts.Version (0 = previous revision).
func RollbackRelease(cfgObj any, name string, opts RollbackOptions) error {
	_, err := withConfig(cfgObj, func(cfg *Config) (struct{}, error) {
		switch opts.ServerSideApply {
		case "", "true", "false", "auto":
		default:
			return struct{}{}, cerrors.New(cerrors.CodeInvalidArg,
				fmt.Sprintf(`invalid server_side_apply %q: must be "true", "false" or "auto"`, opts.ServerSideApply))
		}
		rb := action.NewRollback(cfg.Cfg)
		rb.Version = opts.Version
		rb.WaitForJobs = opts.WaitForJobs
		rb.DisableHooks = opts.DisableHooks
		rb.ForceReplace = opts.ForceReplace
		rb.ForceConflicts = opts.ForceConflicts
		if opts.ServerSideApply != "" {
			rb.ServerSideApply = opts.ServerSideApply // "" keeps the SDK default ("auto")
		}
		rb.CleanupOnFail = opts.CleanupOnFail
		rb.MaxHistory = opts.MaxHistory
		if err := (runTargets{&rb.Timeout, &rb.WaitStrategy, &rb.DryRunStrategy}).apply(opts.TimeoutSeconds, opts.Wait, opts.DryRun); err != nil {
			return struct{}{}, err
		}
		return struct{}{}, wrapActionError(rb.Run(name), cerrors.CodeRelease)
	})
	return err
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

// ListOptions is the JSON options contract of helm_list. Keys are ABI:
// additive only.
type ListOptions struct {
	All           bool   `json:"all"` // every state (limit/offset still apply)
	AllNamespaces bool   `json:"all_namespaces"`
	Limit         int    `json:"limit"`
	Offset        int    `json:"offset"`
	Filter        string `json:"filter"` // regex on release names
	Selector      string `json:"selector"`
	ByDate        bool   `json:"by_date"`
	SortReverse   bool   `json:"sort_reverse"`
	Deployed      bool   `json:"deployed"`
	Failed        bool   `json:"failed"`
	Pending       bool   `json:"pending"`
	Superseded    bool   `json:"superseded"`
	Uninstalled   bool   `json:"uninstalled"`
	Uninstalling  bool   `json:"uninstalling"`
}

// ParseListOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseListOptions(optsJSON string) (ListOptions, error) {
	return decodeOptions[ListOptions](optsJSON, "list")
}

// ListReleases returns a JSON array of release summaries (no manifests).
func ListReleases(cfgObj any, opts ListOptions) (string, error) {
	return withConfig(cfgObj, func(cfg *Config) (string, error) {
		if opts.Filter != "" {
			if _, err := regexp.Compile(opts.Filter); err != nil {
				return "", cerrors.WithCode(cerrors.CodeInvalidArg, fmt.Errorf("invalid filter regex: %w", err))
			}
		}
		// Listing across namespaces needs storage bound to "" — the helm CLI
		// re-initializes its configuration for that; helm-c derives a
		// sibling so the shared handle keeps its namespace.
		actionCfg := cfg.Cfg
		if opts.AllNamespaces {
			var err error
			if actionCfg, err = cfg.forNamespace(""); err != nil {
				return "", err
			}
		}

		l := action.NewList(actionCfg)
		l.All = opts.All
		l.AllNamespaces = opts.AllNamespaces
		l.Limit = opts.Limit
		l.Offset = opts.Offset
		l.Filter = opts.Filter
		l.Selector = opts.Selector
		l.ByDate = opts.ByDate
		l.SortReverse = opts.SortReverse
		l.Deployed = opts.Deployed
		l.Failed = opts.Failed
		l.Pending = opts.Pending
		l.Superseded = opts.Superseded
		l.Uninstalled = opts.Uninstalled
		l.Uninstalling = opts.Uninstalling
		l.SetStateMask()

		return summariesJSON(l.Run())
	})
}

// ---------------------------------------------------------------------------
// Status / history / get
// ---------------------------------------------------------------------------

// StatusOptions is the JSON options contract of helm_status. Keys are ABI:
// additive only.
type StatusOptions struct {
	Revision int `json:"revision"` // 0 = latest
}

// ParseStatusOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseStatusOptions(optsJSON string) (StatusOptions, error) {
	return decodeOptions[StatusOptions](optsJSON, "status")
}

// StatusRelease returns the release summary JSON (with manifest) for name.
func StatusRelease(cfgObj any, name string, opts StatusOptions) (string, error) {
	return withConfig(cfgObj, func(cfg *Config) (string, error) {
		st := action.NewStatus(cfg.Cfg)
		st.Version = opts.Revision
		return finishRelease(st.Run(name))
	})
}

// HistoryOptions is the JSON options contract of helm_history. Keys are ABI:
// additive only.
type HistoryOptions struct {
	Max int `json:"max"` // 0 = every revision
}

// ParseHistoryOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseHistoryOptions(optsJSON string) (HistoryOptions, error) {
	return decodeOptions[HistoryOptions](optsJSON, "history")
}

// HistoryRelease returns a JSON array of revision summaries for name, in
// ascending revision order. max keeps only the newest revisions (the SDK's
// History.Max is never read by History.Run; the truncation lives in the
// helm CLI, so it is reproduced here).
func HistoryRelease(cfgObj any, name string, opts HistoryOptions) (string, error) {
	return withConfig(cfgObj, func(cfg *Config) (string, error) {
		rels, err := action.NewHistory(cfg.Cfg).Run(name)
		if err != nil {
			return "", wrapActionError(err, cerrors.CodeRelease)
		}
		summaries, err := summarizeReleases(rels)
		if err != nil {
			return "", err
		}
		sort.Slice(summaries, func(i, j int) bool { return summaries[i].Revision < summaries[j].Revision })
		if opts.Max > 0 && len(summaries) > opts.Max {
			summaries = summaries[len(summaries)-opts.Max:]
		}
		return marshalJSON(summaries)
	})
}

// GetOptions is the JSON options contract of helm_get. Keys are ABI:
// additive only.
type GetOptions struct {
	Revision int `json:"revision"` // 0 = latest
}

// ParseGetOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseGetOptions(optsJSON string) (GetOptions, error) {
	return decodeOptions[GetOptions](optsJSON, "get")
}

// GetRelease returns the full stored release for name — the summary (with
// manifest) plus its hooks, user-supplied config and info block — as JSON:
// {"summary":{...},"hooks":[...],"config":{...},"info":{...}}.
func GetRelease(cfgObj any, name string, opts GetOptions) (string, error) {
	return withConfig(cfgObj, func(cfg *Config) (string, error) {
		g := action.NewGet(cfg.Cfg)
		g.Version = opts.Revision
		rel, err := g.Run(name)
		if err != nil {
			return "", wrapActionError(err, cerrors.CodeRelease)
		}
		s, err := summarizeRelease(rel, true)
		if err != nil {
			return "", err
		}
		out := map[string]any{"summary": s}
		if v1, ok := rel.(*rspb.Release); ok {
			out["hooks"] = v1.Hooks
			out["config"] = v1.Config
			out["info"] = v1.Info
		}
		return marshalJSON(out)
	})
}

// ---------------------------------------------------------------------------
// Metadata / values
// ---------------------------------------------------------------------------

// GetMetadataOptions is the JSON options contract of helm_get_metadata.
// Keys are ABI: additive only.
type GetMetadataOptions struct {
	Revision int `json:"revision"` // 0 = latest
}

// ParseGetMetadataOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseGetMetadataOptions(optsJSON string) (GetMetadataOptions, error) {
	return decodeOptions[GetMetadataOptions](optsJSON, "get metadata")
}

// GetReleaseMetadata returns the release's metadata (chart, versions,
// annotations, …) as JSON.
func GetReleaseMetadata(cfgObj any, name string, opts GetMetadataOptions) (string, error) {
	return withConfig(cfgObj, func(cfg *Config) (string, error) {
		gm := action.NewGetMetadata(cfg.Cfg)
		gm.Version = opts.Revision
		meta, err := gm.Run(name)
		if err != nil {
			return "", wrapActionError(err, cerrors.CodeRelease)
		}
		return marshalJSON(meta)
	})
}

// GetValuesOptions is the JSON options contract of helm_get_values. Keys are
// ABI: additive only.
type GetValuesOptions struct {
	All      bool `json:"all"` // computed values, not just user-supplied
	Revision int  `json:"revision"`
}

// ParseGetValuesOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseGetValuesOptions(optsJSON string) (GetValuesOptions, error) {
	return decodeOptions[GetValuesOptions](optsJSON, "get values")
}

// GetReleaseValues returns the release's values as JSON.
func GetReleaseValues(cfgObj any, name string, opts GetValuesOptions) (string, error) {
	return withConfig(cfgObj, func(cfg *Config) (string, error) {
		gv := action.NewGetValues(cfg.Cfg)
		gv.AllValues = opts.All
		gv.Version = opts.Revision
		vals, err := gv.Run(name)
		if err != nil {
			return "", wrapActionError(err, cerrors.CodeRelease)
		}
		return marshalJSON(vals)
	})
}

// ---------------------------------------------------------------------------
// Test (helm test)
// ---------------------------------------------------------------------------

// TestOptions is the JSON options contract of helm_test_run. Keys are ABI:
// additive only.
type TestOptions struct {
	TimeoutSeconds int      `json:"timeout_seconds"`
	Logs           bool     `json:"logs"` // collect test pod logs into the result
	IncludeNames   []string `json:"include_names"`
	ExcludeNames   []string `json:"exclude_names"`
}

// ParseTestOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseTestOptions(optsJSON string) (TestOptions, error) {
	return decodeOptions[TestOptions](optsJSON, "test")
}

// TestRelease runs the release's test hooks (`helm test`) and returns
// {"release":{summary},"logs":"..."} — logs are collected only when
// opts.Logs is set. A failing test surfaces as HELM_ERR_RELEASE with the
// SDK's detail; the release summary is still returned in that case is not
// possible over the C ABI, so the summary rides the success path only.
func TestRelease(cfgObj any, name string, opts TestOptions) (string, error) {
	return withConfig(cfgObj, func(cfg *Config) (string, error) {
		rt := action.NewReleaseTesting(cfg.Cfg)
		rt.Namespace = cfg.Namespace
		if opts.TimeoutSeconds > 0 {
			rt.Timeout = time.Duration(opts.TimeoutSeconds) * time.Second
		}
		rt.Filters = map[string][]string{}
		if len(opts.IncludeNames) > 0 {
			rt.Filters[action.IncludeNameFilter] = opts.IncludeNames
		}
		if len(opts.ExcludeNames) > 0 {
			rt.Filters[action.ExcludeNameFilter] = opts.ExcludeNames
		}

		rel, shutdown, runErr := rt.Run(name)
		// The SDK defers pod cleanup so logs can still be read; helm-c reads
		// them (when asked) and then always shuts down, like the CLI.
		var logs bytes.Buffer
		if rel != nil && opts.Logs {
			if v1, ok := rel.(*rspb.Release); ok && hasTestHooks(v1) {
				// Pod logs need a live API connection (the SDK builds a
				// clientset from the REST getter); a config without one
				// cannot have run any test pod either.
				if cfg.Cfg.RESTClientGetter == nil {
					runErr = cerrors.New(cerrors.CodeKube, "test pod logs need a cluster connection")
				} else if err := rt.GetPodLogs(&logs, v1); err != nil && runErr == nil {
					runErr = err
				}
			}
		}
		if shutdown != nil {
			if err := shutdown(); err != nil && runErr == nil {
				runErr = err
			}
		}
		if runErr != nil {
			return "", wrapActionError(runErr, cerrors.CodeRelease)
		}
		s, err := summarizeRelease(rel, false)
		if err != nil {
			return "", err
		}
		return marshalJSON(map[string]any{"release": s, "logs": logs.String()})
	})
}

// hasTestHooks reports whether the release declares any test hook — the
// only kind whose pods `helm test` runs and whose logs it can fetch.
func hasTestHooks(rel *rspb.Release) bool {
	for _, h := range rel.Hooks {
		for _, e := range h.Events {
			if e == rspb.HookTest {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Cluster probe / registry client binding
// ---------------------------------------------------------------------------

// CheckReachable reports whether the config's cluster answers (the same
// probe every action runs first). An unreachable cluster is HELM_ERR_KUBE.
func CheckReachable(cfgObj any) error {
	_, err := withConfig(cfgObj, func(cfg *Config) (struct{}, error) {
		if err := cfg.Cfg.KubeClient.IsReachable(); err != nil {
			return struct{}{}, cerrors.WithCode(cerrors.CodeKube, err)
		}
		return struct{}{}, nil
	})
	return err
}

// SetConfigRegistryClient binds a registry client to the config so that
// install/upgrade by an oci:// chart_ref use its credentials. nil unbinds.
func SetConfigRegistryClient(cfgObj, clientObj any) error {
	_, err := withConfig(cfgObj, func(cfg *Config) (struct{}, error) {
		if clientObj == nil {
			cfg.Cfg.RegistryClient = nil
			return struct{}{}, nil
		}
		c, err := AsRegistryClient(clientObj)
		if err != nil {
			return struct{}{}, err
		}
		cfg.Cfg.RegistryClient = c
		return struct{}{}, nil
	})
	return err
}
