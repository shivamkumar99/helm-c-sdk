package wrapper

import (
	"helm.sh/helm/v4/pkg/chart/common"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	"helm.sh/helm/v4/pkg/engine"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// RenderOptions is the JSON options contract of helm_render. Keys are ABI:
// additive only, documented in docs/API.md.
type RenderOptions struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Revision  int    `json:"revision"`
	IsInstall bool   `json:"is_install"`
	IsUpgrade bool   `json:"is_upgrade"`
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

// RenderChart renders the chart's templates offline (no cluster; the lookup
// template function returns empty results) and returns
// {"path/to/template.yaml": "rendered manifest", ...} as JSON.
func RenderChart(chartObj any, valuesJSON string, opts RenderOptions) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	vals, err := unmarshalValues(valuesJSON)
	if err != nil {
		return "", err
	}

	releaseOpts := common.ReleaseOptions{
		Name:      opts.Name,
		Namespace: opts.Namespace,
		Revision:  opts.Revision,
		IsInstall: opts.IsInstall,
		IsUpgrade: opts.IsUpgrade,
	}
	renderValues, err := commonutil.ToRenderValues(c, vals, releaseOpts, common.DefaultCapabilities)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}

	manifests, err := engine.Render(c, renderValues)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRender, err)
	}
	return marshalJSON(manifests)
}
