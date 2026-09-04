package wrapper

import (
	"bytes"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/release"
	rspb "helm.sh/helm/v4/pkg/release/v1"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

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
		rt := newReleaseTesting(cfg, opts)
		rel, shutdown, runErr := rt.Run(name)
		// The SDK defers pod cleanup so logs can still be read; helm-c reads
		// them (when asked) and then always shuts down, like the CLI.
		logs, logErr := collectTestLogs(cfg, rt, rel, opts.Logs)
		runErr = firstError(runErr, logErr)
		if shutdown != nil {
			runErr = firstError(runErr, shutdown())
		}
		if runErr != nil {
			return "", wrapActionError(runErr, cerrors.CodeRelease)
		}
		s, err := summarizeRelease(rel, false)
		if err != nil {
			return "", err
		}
		return marshalJSON(map[string]any{"release": s, "logs": logs})
	})
}

// newReleaseTesting builds the SDK action from the options.
func newReleaseTesting(cfg *Config, opts TestOptions) *action.ReleaseTesting {
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
	return rt
}

// collectTestLogs fetches the test pods' logs when asked and there is
// anything to fetch. Pod logs need a live API connection (the SDK builds a
// clientset from the REST getter); a config without one cannot have run any
// test pod either.
func collectTestLogs(cfg *Config, rt *action.ReleaseTesting, rel release.Releaser, wanted bool) (string, error) {
	if !wanted || rel == nil {
		return "", nil
	}
	v1, ok := rel.(*rspb.Release)
	if !ok || !hasTestHooks(v1) {
		return "", nil
	}
	if cfg.Cfg.RESTClientGetter == nil {
		return "", cerrors.New(cerrors.CodeKube, "test pod logs need a cluster connection")
	}
	var logs bytes.Buffer
	err := rt.GetPodLogs(&logs, v1)
	return logs.String(), err
}

// firstError keeps the earliest failure of a sequence.
func firstError(first, next error) error {
	if first != nil {
		return first
	}
	return next
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
