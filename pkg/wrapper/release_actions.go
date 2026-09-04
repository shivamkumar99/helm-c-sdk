package wrapper

import (
	"fmt"
	"regexp"
	"sort"

	"helm.sh/helm/v4/pkg/action"
	rspb "helm.sh/helm/v4/pkg/release/v1"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

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
		if err := validateServerSideApply(opts.ServerSideApply); err != nil {
			return struct{}{}, err
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
