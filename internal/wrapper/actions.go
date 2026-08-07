package wrapper

import (
	"context"
	"errors"
	"time"

	"helm.sh/helm/v4/pkg/action"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/registry"
	"helm.sh/helm/v4/pkg/storage/driver"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

// chartForAction returns the chart to operate on: either a loaded chart
// handle object, or — when chartObj is nil — a chart reference resolved the
// same way the helm CLI does (local path, repo chart name via
// cpo.RepoURL, or an oci:// reference) through the SDK's LocateChart.
// setClient installs a default registry client for anonymous oci:// refs
// when the config carries none.
func chartForAction(cfg *Config, chartObj any, chartRef string, cpo *action.ChartPathOptions,
	setClient func(*registry.Client), plainHTTP bool) (*chart.Chart, error) {
	if chartObj != nil {
		return AsChart(chartObj)
	}
	if chartRef == "" {
		return nil, cerrors.New(cerrors.CodeInvalidArg,
			"either a chart handle or a chart_ref is required")
	}
	if registry.IsOCI(chartRef) && cfg.Cfg.RegistryClient == nil {
		client, err := NewRegistryClient(RegistryClientOptions{PlainHTTP: plainHTTP})
		if err != nil {
			return nil, err
		}
		setClient(client)
	}
	path, err := cpo.LocateChart(chartRef, cli.New())
	if err != nil {
		return nil, cerrors.WithCode(cerrors.CodeChartLoad, err)
	}
	return LoadChart(path)
}

// wrapActionError classifies an action failure: cancellation and not-found
// keep their dedicated codes through any wrapping; everything else gets the
// action's fallback code.
func wrapActionError(err error, fallback cerrors.Code) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
		return cerrors.WithCode(cerrors.CodeCancelled, err)
	case errors.Is(err, driver.ErrReleaseNotFound):
		return cerrors.WithCode(cerrors.CodeNotFound, err)
	}
	return cerrors.WithCode(fallback, err)
}

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
	// Chart-reference resolution (used when installing by chart_ref):
	ChartRepoURL string `json:"chart_repo_url"`
	ChartVersion string `json:"chart_version"`
	PlainHTTP    bool   `json:"plain_http"`
}

// ParseInstallOptions strictly decodes optsJSON (ADR-0004).
func ParseInstallOptions(optsJSON string) (InstallOptions, error) {
	return decodeOptions[InstallOptions](optsJSON, "install")
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

// InstallRelease installs a chart (loaded handle object, or chartRef when
// chartObj is nil) as name and returns the release summary JSON (with
// manifest). ctx cancels the operation (HELM_ERR_CANCELLED).
func InstallRelease(ctx context.Context, cfgObj, chartObj any, chartRef, name, valuesJSON string, opts InstallOptions) (string, error) {
	if err := checkNotCancelled(ctx); err != nil {
		return "", err
	}
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		return "", err
	}
	vals, err := unmarshalValues(valuesJSON)
	if err != nil {
		return "", err
	}

	inst := action.NewInstall(cfg.Cfg)
	inst.RepoURL = opts.ChartRepoURL
	inst.Version = opts.ChartVersion
	inst.PlainHTTP = opts.PlainHTTP
	c, err := chartForAction(cfg, chartObj, chartRef, &inst.ChartPathOptions, inst.SetRegistryClient, opts.PlainHTTP)
	if err != nil {
		return "", err
	}
	inst.ReleaseName = name
	inst.Namespace = opts.Namespace
	if inst.Namespace == "" {
		inst.Namespace = cfg.Namespace
	}
	if opts.TimeoutSeconds > 0 {
		inst.Timeout = time.Duration(opts.TimeoutSeconds) * time.Second
	}
	if opts.Wait != "" {
		inst.WaitStrategy = kube.WaitStrategy(opts.Wait)
	}
	if opts.DryRun != "" {
		inst.DryRunStrategy = action.DryRunStrategy(opts.DryRun)
	}
	inst.CreateNamespace = opts.CreateNamespace
	inst.RollbackOnFailure = opts.RollbackOnFailure
	inst.Description = opts.Description
	inst.Labels = opts.Labels

	rel, err := inst.RunWithContext(ctx, c, vals)
	if err != nil {
		return "", wrapActionError(err, cerrors.CodeRelease)
	}
	s, err := summarizeRelease(rel, true)
	if err != nil {
		return "", err
	}
	return marshalJSON(s)
}

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
	// Chart-reference resolution (used when upgrading by chart_ref):
	ChartRepoURL string `json:"chart_repo_url"`
	ChartVersion string `json:"chart_version"`
	PlainHTTP    bool   `json:"plain_http"`
}

// ParseUpgradeOptions strictly decodes optsJSON (ADR-0004).
func ParseUpgradeOptions(optsJSON string) (UpgradeOptions, error) {
	return decodeOptions[UpgradeOptions](optsJSON, "upgrade")
}

// UpgradeRelease upgrades name to a chart (loaded handle object, or chartRef
// when chartObj is nil) and returns the release summary JSON (with manifest).
// ctx cancels the operation.
func UpgradeRelease(ctx context.Context, cfgObj, chartObj any, chartRef, name, valuesJSON string, opts UpgradeOptions) (string, error) {
	if err := checkNotCancelled(ctx); err != nil {
		return "", err
	}
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		return "", err
	}
	vals, err := unmarshalValues(valuesJSON)
	if err != nil {
		return "", err
	}

	up := action.NewUpgrade(cfg.Cfg)
	up.RepoURL = opts.ChartRepoURL
	up.Version = opts.ChartVersion
	up.PlainHTTP = opts.PlainHTTP
	c, err := chartForAction(cfg, chartObj, chartRef, &up.ChartPathOptions, up.SetRegistryClient, opts.PlainHTTP)
	if err != nil {
		return "", err
	}
	up.Namespace = opts.Namespace
	if up.Namespace == "" {
		up.Namespace = cfg.Namespace
	}
	if opts.TimeoutSeconds > 0 {
		up.Timeout = time.Duration(opts.TimeoutSeconds) * time.Second
	}
	if opts.Wait != "" {
		up.WaitStrategy = kube.WaitStrategy(opts.Wait)
	}
	if opts.DryRun != "" {
		up.DryRunStrategy = action.DryRunStrategy(opts.DryRun)
	}
	up.MaxHistory = opts.MaxHistory
	up.ResetValues = opts.ResetValues
	up.ReuseValues = opts.ReuseValues
	up.CleanupOnFail = opts.CleanupOnFail
	up.RollbackOnFailure = opts.RollbackOnFailure
	up.Description = opts.Description
	up.Labels = opts.Labels

	rel, err := up.RunWithContext(ctx, name, c, vals)
	if err != nil {
		return "", wrapActionError(err, cerrors.CodeRelease)
	}
	s, err := summarizeRelease(rel, true)
	if err != nil {
		return "", err
	}
	return marshalJSON(s)
}

// UninstallOptions is the JSON options contract of helm_uninstall. Keys are
// ABI: additive only.
type UninstallOptions struct {
	KeepHistory    bool   `json:"keep_history"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	DryRun         bool   `json:"dry_run"`
	IgnoreNotFound bool   `json:"ignore_not_found"`
	Wait           string `json:"wait"`
	Description    string `json:"description"`
}

// ParseUninstallOptions strictly decodes optsJSON (ADR-0004).
func ParseUninstallOptions(optsJSON string) (UninstallOptions, error) {
	return decodeOptions[UninstallOptions](optsJSON, "uninstall")
}

// UninstallRelease removes name and returns {"info": "...", "release": {...}}.
func UninstallRelease(cfgObj any, name string, opts UninstallOptions) (string, error) {
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		return "", err
	}

	u := action.NewUninstall(cfg.Cfg)
	u.KeepHistory = opts.KeepHistory
	if opts.TimeoutSeconds > 0 {
		u.Timeout = time.Duration(opts.TimeoutSeconds) * time.Second
	}
	u.DryRun = opts.DryRun
	u.IgnoreNotFound = opts.IgnoreNotFound
	if opts.Wait != "" {
		u.WaitStrategy = kube.WaitStrategy(opts.Wait)
	}
	u.Description = opts.Description

	resp, err := u.Run(name)
	if err != nil {
		return "", wrapActionError(err, cerrors.CodeRelease)
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
}

// RollbackOptions is the JSON options contract of helm_rollback. Keys are
// ABI: additive only.
type RollbackOptions struct {
	Version        int    `json:"version"` // 0 = previous revision
	TimeoutSeconds int    `json:"timeout_seconds"`
	Wait           string `json:"wait"`
	DryRun         string `json:"dry_run"`
}

// ParseRollbackOptions strictly decodes optsJSON (ADR-0004).
func ParseRollbackOptions(optsJSON string) (RollbackOptions, error) {
	return decodeOptions[RollbackOptions](optsJSON, "rollback")
}

// RollbackRelease rolls name back to opts.Version (0 = previous revision).
func RollbackRelease(cfgObj any, name string, opts RollbackOptions) error {
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		return err
	}

	rb := action.NewRollback(cfg.Cfg)
	rb.Version = opts.Version
	if opts.TimeoutSeconds > 0 {
		rb.Timeout = time.Duration(opts.TimeoutSeconds) * time.Second
	}
	if opts.Wait != "" {
		rb.WaitStrategy = kube.WaitStrategy(opts.Wait)
	}
	if opts.DryRun != "" {
		rb.DryRunStrategy = action.DryRunStrategy(opts.DryRun)
	}
	return wrapActionError(rb.Run(name), cerrors.CodeRelease)
}

// ListOptions is the JSON options contract of helm_list. Keys are ABI:
// additive only.
type ListOptions struct {
	All           bool   `json:"all"` // every state, ignore limit/offset
	AllNamespaces bool   `json:"all_namespaces"`
	Limit         int    `json:"limit"`
	Offset        int    `json:"offset"`
	Filter        string `json:"filter"` // regex on release names
}

// ParseListOptions strictly decodes optsJSON (ADR-0004).
func ParseListOptions(optsJSON string) (ListOptions, error) {
	return decodeOptions[ListOptions](optsJSON, "list")
}

// ListReleases returns a JSON array of release summaries (no manifests).
func ListReleases(cfgObj any, opts ListOptions) (string, error) {
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		return "", err
	}

	l := action.NewList(cfg.Cfg)
	l.All = opts.All
	if opts.All {
		l.StateMask = action.ListAll
	}
	l.AllNamespaces = opts.AllNamespaces
	l.Limit = opts.Limit
	l.Offset = opts.Offset
	l.Filter = opts.Filter
	l.SetStateMask()

	rels, err := l.Run()
	if err != nil {
		return "", wrapActionError(err, cerrors.CodeRelease)
	}
	summaries, err := summarizeReleases(rels)
	if err != nil {
		return "", err
	}
	return marshalJSON(summaries)
}

// StatusOptions is the JSON options contract of helm_status. Keys are ABI:
// additive only.
type StatusOptions struct {
	Revision int `json:"revision"` // 0 = latest
}

// ParseStatusOptions strictly decodes optsJSON (ADR-0004).
func ParseStatusOptions(optsJSON string) (StatusOptions, error) {
	return decodeOptions[StatusOptions](optsJSON, "status")
}

// StatusRelease returns the release summary JSON (with manifest) for name.
func StatusRelease(cfgObj any, name string, opts StatusOptions) (string, error) {
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		return "", err
	}

	st := action.NewStatus(cfg.Cfg)
	st.Version = opts.Revision
	rel, err := st.Run(name)
	if err != nil {
		return "", wrapActionError(err, cerrors.CodeRelease)
	}
	s, err := summarizeRelease(rel, true)
	if err != nil {
		return "", err
	}
	return marshalJSON(s)
}

// HistoryOptions is the JSON options contract of helm_history. Keys are ABI:
// additive only.
type HistoryOptions struct {
	Max int `json:"max"` // 0 = SDK default
}

// ParseHistoryOptions strictly decodes optsJSON (ADR-0004).
func ParseHistoryOptions(optsJSON string) (HistoryOptions, error) {
	return decodeOptions[HistoryOptions](optsJSON, "history")
}

// HistoryRelease returns a JSON array of revision summaries for name.
func HistoryRelease(cfgObj any, name string, opts HistoryOptions) (string, error) {
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		return "", err
	}

	h := action.NewHistory(cfg.Cfg)
	if opts.Max > 0 {
		h.Max = opts.Max
	}
	rels, err := h.Run(name)
	if err != nil {
		return "", wrapActionError(err, cerrors.CodeRelease)
	}
	summaries, err := summarizeReleases(rels)
	if err != nil {
		return "", err
	}
	return marshalJSON(summaries)
}

// GetMetadataOptions is the JSON options contract of helm_get_metadata.
// Keys are ABI: additive only.
type GetMetadataOptions struct {
	Revision int `json:"revision"` // 0 = latest
}

// ParseGetMetadataOptions strictly decodes optsJSON (ADR-0004).
func ParseGetMetadataOptions(optsJSON string) (GetMetadataOptions, error) {
	return decodeOptions[GetMetadataOptions](optsJSON, "get metadata")
}

// GetReleaseMetadata returns the release's metadata (chart, versions,
// annotations, …) as JSON.
func GetReleaseMetadata(cfgObj any, name string, opts GetMetadataOptions) (string, error) {
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		return "", err
	}

	gm := action.NewGetMetadata(cfg.Cfg)
	gm.Version = opts.Revision
	meta, err := gm.Run(name)
	if err != nil {
		return "", wrapActionError(err, cerrors.CodeRelease)
	}
	return marshalJSON(meta)
}

// GetValuesOptions is the JSON options contract of helm_get_values. Keys are
// ABI: additive only.
type GetValuesOptions struct {
	All      bool `json:"all"` // computed values, not just user-supplied
	Revision int  `json:"revision"`
}

// ParseGetValuesOptions strictly decodes optsJSON (ADR-0004).
func ParseGetValuesOptions(optsJSON string) (GetValuesOptions, error) {
	return decodeOptions[GetValuesOptions](optsJSON, "get values")
}

// GetReleaseValues returns the release's values as JSON.
func GetReleaseValues(cfgObj any, name string, opts GetValuesOptions) (string, error) {
	cfg, err := AsConfig(cfgObj)
	if err != nil {
		return "", err
	}

	gv := action.NewGetValues(cfg.Cfg)
	gv.AllValues = opts.All
	gv.Version = opts.Revision
	vals, err := gv.Run(name)
	if err != nil {
		return "", wrapActionError(err, cerrors.CodeRelease)
	}
	return marshalJSON(vals)
}
