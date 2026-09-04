package wrapper

import (
	"helm.sh/helm/v4/pkg/chart/common"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"helm.sh/helm/v4/pkg/engine"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// RenderOptions is the JSON options contract of helm_render. Keys are ABI:
// additive only, documented in docs/API.md.
type RenderOptions struct {
	Name        string   `json:"name"`
	Namespace   string   `json:"namespace"`
	Revision    int      `json:"revision"`
	IsInstall   bool     `json:"is_install"`
	IsUpgrade   bool     `json:"is_upgrade"`
	KubeVersion string   `json:"kube_version"` // e.g. "v1.30.0"; default = SDK default capabilities
	APIVersions []string `json:"api_versions"` // extra Capabilities.APIVersions entries
	Strict      bool     `json:"strict"`       // fail on references to missing values
	EnableDNS   bool     `json:"enable_dns"`   // allow DNS lookups in templates
}

const (
	defaultRenderName      = "release-name"
	defaultRenderNamespace = "default"
	defaultRenderRevision  = 1
)

// ParseRenderOptions strictly decodes optsJSON (docs/DESIGN.md §4) and applies
// helm-template-like defaults.
func ParseRenderOptions(optsJSON string) (RenderOptions, error) {
	opts, err := decodeOptions[RenderOptions](optsJSON, "render")
	if err != nil {
		return opts, err
	}
	if opts.Name == "" {
		opts.Name = defaultRenderName
	}
	if opts.Namespace == "" {
		opts.Namespace = defaultRenderNamespace
	}
	if opts.Revision == 0 {
		opts.Revision = defaultRenderRevision
	}
	if !opts.IsInstall && !opts.IsUpgrade {
		opts.IsInstall = true
	}
	return opts, nil
}

// renderCapabilities derives the capabilities a render runs under: the
// SDK defaults, with the requested Kubernetes version and any extra API
// versions applied — exactly what `helm template --kube-version
// --api-versions` does.
func renderCapabilities(opts RenderOptions) (*common.Capabilities, error) {
	caps := common.DefaultCapabilities.Copy()
	kv, err := parseKubeVersion(opts.KubeVersion)
	if err != nil {
		return nil, err
	}
	if kv != nil {
		caps.KubeVersion = *kv
	}
	caps.APIVersions = append(caps.APIVersions, opts.APIVersions...)
	return caps, nil
}

// RenderChart renders the chart's templates offline (no cluster; the lookup
// template function returns empty results) and returns
// {"path/to/template.yaml": "rendered manifest", ...} as JSON.
//
// Dependency conditions, tags and import-values are processed first, as an
// install would (chartutil.ProcessDependencies) — the SDK's engine alone
// renders every subchart regardless of its condition. Like the SDK's own
// install, this processing updates the loaded chart in place.
func RenderChart(chartObj any, valuesJSON string, opts RenderOptions) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	vals, err := unmarshalValues(valuesJSON)
	if err != nil {
		return "", err
	}
	caps, err := renderCapabilities(opts)
	if err != nil {
		return "", err
	}
	if err := chartutil.ProcessDependencies(c, vals); err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}

	releaseOpts := common.ReleaseOptions{
		Name:      opts.Name,
		Namespace: opts.Namespace,
		Revision:  opts.Revision,
		IsInstall: opts.IsInstall,
		IsUpgrade: opts.IsUpgrade,
	}
	renderValues, err := commonutil.ToRenderValues(c, vals, releaseOpts, caps)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}

	eng := engine.Engine{Strict: opts.Strict, EnableDNS: opts.EnableDNS}
	manifests, err := eng.Render(c, renderValues)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRender, err)
	}
	return marshalJSON(manifests)
}

// RenderChartWithConfig renders like RenderChart but against the cluster
// behind cfgObj, so the `lookup` template function returns live objects
// (SDK engine.RenderWithClient). Nothing is created or stored.
func RenderChartWithConfig(cfgObj, chartObj any, valuesJSON string, opts RenderOptions) (string, error) {
	return withConfig(cfgObj, func(cfg *Config) (string, error) {
		c, err := AsChart(chartObj)
		if err != nil {
			return "", err
		}
		vals, err := unmarshalValues(valuesJSON)
		if err != nil {
			return "", err
		}
		caps, err := renderCapabilities(opts)
		if err != nil {
			return "", err
		}
		if err := chartutil.ProcessDependencies(c, vals); err != nil {
			return "", cerrors.WithCode(cerrors.CodeValues, err)
		}
		restConfig, err := cfg.Cfg.RESTClientGetter.ToRESTConfig()
		if err != nil {
			return "", cerrors.WithCode(cerrors.CodeKube, err)
		}
		renderValues, err := commonutil.ToRenderValues(c, vals, common.ReleaseOptions{
			Name:      opts.Name,
			Namespace: opts.Namespace,
			Revision:  opts.Revision,
			IsInstall: opts.IsInstall,
			IsUpgrade: opts.IsUpgrade,
		}, caps)
		if err != nil {
			return "", cerrors.WithCode(cerrors.CodeValues, err)
		}
		manifests, err := engine.RenderWithClient(c, renderValues, restConfig)
		if err != nil {
			return "", cerrors.WithCode(cerrors.CodeRender, err)
		}
		return marshalJSON(manifests)
	})
}
