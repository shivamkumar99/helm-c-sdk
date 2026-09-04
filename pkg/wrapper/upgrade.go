package wrapper

import (
	"context"
	"fmt"

	"helm.sh/helm/v4/pkg/action"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

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
	up, err := newUpgrade(cfg, opts)
	if err != nil {
		return "", err
	}
	c, err := chartForInstall(cfg, chartObj, chartRef, opts.ChartRefOptions)
	if err != nil {
		return "", err
	}
	rel, err := up.RunWithContext(ctx, name, c, vals)
	return finishRelease(rel, err)
}

// validateServerSideApply checks the tri-state string the SDK's upgrade and
// rollback actions take.
func validateServerSideApply(s string) error {
	switch s {
	case "", "true", "false", "auto":
		return nil
	}
	return cerrors.New(cerrors.CodeInvalidArg,
		fmt.Sprintf(`invalid server_side_apply %q: must be "true", "false" or "auto"`, s))
}

// newUpgrade builds the SDK upgrade action. An upgrade always operates in
// the namespace the release lives in — the SDK reads it from the stored
// release and ignores Upgrade.Namespace — so a different value would be
// silently ignored and is rejected instead.
func newUpgrade(cfg *Config, opts UpgradeOptions) (*action.Upgrade, error) {
	if opts.Namespace != "" && opts.Namespace != cfg.Namespace {
		return nil, cerrors.New(cerrors.CodeInvalidArg, fmt.Sprintf(
			"upgrade cannot change namespace (config is %q, got %q); create a config for that namespace",
			cfg.Namespace, opts.Namespace))
	}
	if err := validateServerSideApply(opts.ServerSideApply); err != nil {
		return nil, err
	}
	up := action.NewUpgrade(cfg.Cfg)
	up.Namespace = cfg.Namespace
	if err := (runTargets{&up.Timeout, &up.WaitStrategy, &up.DryRunStrategy}).apply(opts.TimeoutSeconds, opts.Wait, opts.DryRun); err != nil {
		return nil, err
	}
	return up, applyUpgradeOptions(up, opts)
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
