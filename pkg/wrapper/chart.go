// Package wrapper is the pure-Go layer over the Helm SDK that the C ABI is
// built on — and a public Go API in its own right: Go programs can import it
// directly for a simplified, JSON-oriented Helm interface (github.com/
// shivamkumar99/helm-c-sdk/pkg/wrapper) with stable error codes from
// pkg/cerrors. All logic lives here, fully testable without cgo; the capi
// package only converts and delegates. Loggers are injected — never global.
package wrapper

import (
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// ValidateReleaseName reports whether name is usable as a Helm release name,
// under the SDK's own rules (chartutil.ValidateReleaseName).
func ValidateReleaseName(name string) error {
	return cerrors.WithCode(cerrors.CodeInvalidArg, chartutil.ValidateReleaseName(name))
}

// LoadChart loads a chart from a directory or .tgz archive.
func LoadChart(path string) (*chart.Chart, error) {
	c, err := loader.Load(path)
	if err != nil {
		return nil, cerrors.WithCode(cerrors.CodeChartLoad, err)
	}
	return c, nil
}

// AsChart recovers the concrete chart from a registry object, keeping SDK
// types out of the capi package entirely.
func AsChart(obj any) (*chart.Chart, error) {
	c, ok := obj.(*chart.Chart)
	if !ok {
		return nil, cerrors.New(cerrors.CodeWrongHandleType, "handle does not hold a chart")
	}
	return c, nil
}

// ChartMetadataJSON renders the chart's Chart.yaml metadata as JSON.
func ChartMetadataJSON(chartObj any) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	return marshalJSON(c.Metadata)
}

// ChartValuesJSON renders the chart's default values as JSON.
func ChartValuesJSON(chartObj any) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	return marshalJSON(c.Values)
}

// SaveChart archives the chart into destDir and returns the .tgz path.
func SaveChart(chartObj any, destDir string) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	path, err := chartutil.Save(c, destDir)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeIO, err)
	}
	return path, nil
}

// CreateChart scaffolds a new chart named name inside dir and returns the
// created chart directory.
func CreateChart(name, dir string) (string, error) {
	// ValidateMetadataName is deprecated upstream but present and correct in
	// the pinned SDK release; revisit at the next pin bump.
	if err := chartutil.ValidateMetadataName(name); err != nil { //nolint:staticcheck
		return "", cerrors.WithCode(cerrors.CodeInvalidArg, err)
	}
	path, err := chartutil.Create(name, dir)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeIO, err)
	}
	return path, nil
}
