package wrapper

import (
	"context"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
)

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
	inst, err := newInstall(cfg, name, opts)
	if err != nil {
		return "", err
	}
	c, err := chartForInstall(cfg, chartObj, chartRef, opts.ChartRefOptions)
	if err != nil {
		return "", err
	}
	rel, err := inst.RunWithContext(ctx, c, vals)
	return finishRelease(rel, err)
}

// newInstall builds the SDK install action on the right configuration. The
// storage and kube client are bound to a namespace at Init, so an install
// into another namespace needs a configuration for it; and a client-side dry
// run mutates its Configuration (swaps in fakes), so it must never see the
// shared one.
func newInstall(cfg *Config, name string, opts InstallOptions) (*action.Install, error) {
	dryRun, err := parseDryRun(opts.DryRun)
	if err != nil {
		return nil, err
	}
	namespace := opts.Namespace
	if namespace == "" {
		namespace = cfg.Namespace
	}
	actionCfg, err := cfg.forNamespace(namespace)
	if err != nil {
		return nil, err
	}
	if !interactsWithServer(dryRun) {
		actionCfg = (&Config{Cfg: actionCfg, Namespace: namespace, opts: cfg.opts}).detachedConfiguration()
	}

	inst := action.NewInstall(actionCfg)
	inst.ReleaseName = name
	inst.Namespace = namespace
	inst.DryRunStrategy = dryRun
	if err := (runTargets{&inst.Timeout, &inst.WaitStrategy, nil}).apply(opts.TimeoutSeconds, opts.Wait, ""); err != nil {
		return nil, err
	}
	return inst, applyInstallOptions(inst, opts)
}

// chartForInstall resolves the chart for install/upgrade and enforces the
// CLI's dependency pre-flight.
func chartForInstall(cfg *Config, chartObj any, chartRef string, opts ChartRefOptions) (*chart.Chart, error) {
	rc, err := chartForAction(cfg, chartObj, chartRef, opts)
	if err != nil {
		return nil, err
	}
	return ensureDependencies(rc, opts.DependencyUpdate, opts)
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
