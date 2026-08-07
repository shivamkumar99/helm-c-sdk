package wrapper

import (
	"helm.sh/helm/v4/pkg/action"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

// PackageOptions is the JSON options contract of helm_package_run. Keys are
// ABI: additive only, documented in docs/API.md.
type PackageOptions struct {
	Destination string `json:"destination"`
	Version     string `json:"version"`
	AppVersion  string `json:"app_version"`
}

// ParsePackageOptions strictly decodes optsJSON (ADR-0004).
func ParsePackageOptions(optsJSON string) (PackageOptions, error) {
	return decodeOptions[PackageOptions](optsJSON, "package")
}

// PackageChart archives the chart at path into a .tgz and returns the
// archive path.
func PackageChart(path string, opts PackageOptions) (string, error) {
	p := action.NewPackage()
	p.Destination = opts.Destination
	if p.Destination == "" {
		p.Destination = "."
	}
	p.Version = opts.Version
	p.AppVersion = opts.AppVersion

	out, err := p.Run(path, nil)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeChartInvalid, err)
	}
	return out, nil
}
