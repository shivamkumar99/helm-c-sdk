// Package wrapper is the pure-Go layer over the Helm SDK that the C ABI is
// built on — and a public Go API in its own right: Go programs can import it
// directly for a simplified, JSON-oriented Helm interface (github.com/
// shivamkumar99/helm-c-sdk/pkg/wrapper) with stable error codes from
// pkg/cerrors. All logic lives here, fully testable without cgo; the capi
// package only converts and delegates. Loggers are injected — never global.
package wrapper

import (
	"bytes"
	"fmt"
	"os"

	"helm.sh/helm/v4/pkg/action"
	chartpkg "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"helm.sh/helm/v4/pkg/provenance"
	"helm.sh/helm/v4/pkg/registry"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// as recovers the concrete T behind a registry object. The handle registry
// already guarantees the type by tag, so this is the single defensive check
// standing between the C boundary and the SDK types.
func as[T any](obj any, what string) (T, error) {
	v, ok := obj.(T)
	if !ok {
		var zero T
		return zero, cerrors.New(cerrors.CodeWrongHandleType, "handle does not hold "+what)
	}
	return v, nil
}

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

// LoadChartArchive loads a chart from the bytes of a .tgz archive held in
// memory — no filesystem round trip.
func LoadChartArchive(data []byte) (*chart.Chart, error) {
	c, err := loader.LoadArchive(bytes.NewReader(data))
	if err != nil {
		return nil, cerrors.WithCode(cerrors.CodeChartLoad, err)
	}
	return c, nil
}

// AsChart recovers the concrete chart from a registry object, keeping SDK
// types out of the capi package entirely.
func AsChart(obj any) (*chart.Chart, error) {
	return as[*chart.Chart](obj, "a chart")
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

// chartFile is the stable JSON shape of a chart file crossing the boundary:
// contents ride as a UTF-8 string (chart files are text by convention).
type chartFile struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

func filesJSON(files []*common.File) (string, error) {
	out := make([]chartFile, 0, len(files))
	for _, f := range files {
		out = append(out, chartFile{Name: f.Name, Data: string(f.Data)})
	}
	return marshalJSON(out)
}

// ChartFilesJSON returns the chart's non-template files (README, LICENSE,
// NOTES source, …) as [{"name","data"}] JSON.
func ChartFilesJSON(chartObj any) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	return filesJSON(c.Files)
}

// ChartTemplatesJSON returns the chart's raw templates as [{"name","data"}].
func ChartTemplatesJSON(chartObj any) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	return filesJSON(c.Templates)
}

// ChartCRDsJSON returns the CRDs shipped under crds/ by the chart and its
// subcharts as [{"name","filename","data"}] (SDK Chart.CRDObjects).
func ChartCRDsJSON(chartObj any) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	type crd struct {
		Name     string `json:"name"`
		Filename string `json:"filename"`
		Data     string `json:"data"`
	}
	crds := c.CRDObjects()
	out := make([]crd, 0, len(crds))
	for _, o := range crds {
		data := ""
		if o.File != nil {
			data = string(o.File.Data)
		}
		out = append(out, crd{Name: o.Name, Filename: o.Filename, Data: data})
	}
	return marshalJSON(out)
}

// ChartSchemaJSON returns the chart's values.schema.json document, or
// "null" when the chart ships none.
func ChartSchemaJSON(chartObj any) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	if len(c.Schema) == 0 {
		return "null", nil
	}
	return string(c.Schema), nil
}

// ChartDependenciesJSON returns the metadata of the subcharts actually
// loaded with the chart (charts/), as a JSON array of Chart.yaml objects —
// distinct from the declared dependencies in helm_chart_metadata.
func ChartDependenciesJSON(chartObj any) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	deps := c.Dependencies()
	out := make([]*chart.Metadata, 0, len(deps))
	for _, d := range deps {
		out = append(out, d.Metadata)
	}
	return marshalJSON(out)
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

// SaveChartDir writes the chart back as a directory tree under destDir
// (creating destDir/<name>) and returns that directory.
func SaveChartDir(chartObj any, destDir string) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	if err := chartutil.SaveDir(c, destDir); err != nil {
		return "", cerrors.WithCode(cerrors.CodeIO, err)
	}
	return destDir + string(os.PathSeparator) + c.Name(), nil
}

// ExpandChartArchive unpacks a local .tgz chart archive into destDir
// (`helm pull --untar` for an archive already on disk).
func ExpandChartArchive(destDir, archivePath string) error {
	if err := chartutil.ExpandFile(destDir, archivePath); err != nil {
		return cerrors.WithCode(cerrors.CodeChartLoad, err)
	}
	return nil
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

// CreateChartFrom scaffolds a new chart named name inside dir from the
// starter chart at starterDir (`helm create --starter`), returning the
// created chart directory.
func CreateChartFrom(name, dir, starterDir string) (string, error) {
	if err := chartutil.ValidateMetadataName(name); err != nil { //nolint:staticcheck
		return "", cerrors.WithCode(cerrors.CodeInvalidArg, err)
	}
	// CreateFrom saves through SaveDir, which itself creates dir/<name>.
	meta := &chart.Metadata{Name: name, APIVersion: chart.APIVersionV2, Version: "0.1.0", AppVersion: "0.1.0"}
	if err := chartutil.CreateFrom(meta, dir, starterDir); err != nil {
		return "", cerrors.WithCode(cerrors.CodeIO, err)
	}
	return dir + string(os.PathSeparator) + name, nil
}

// ChartDigest returns the "sha256:..." digest of a chart archive file, the
// value repository indexes carry for each entry (SDK provenance.DigestFile).
func ChartDigest(archivePath string) (string, error) {
	sum, err := provenance.DigestFile(archivePath)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeIO, err)
	}
	return "sha256:" + sum, nil
}

// ValuesFromYAML parses a YAML values document (the -f/--values input) into
// the JSON object every other function accepts (SDK common.ReadValues).
func ValuesFromYAML(yamlText string) (string, error) {
	vals, err := common.ReadValues([]byte(yamlText))
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}
	return marshalJSON(vals.AsMap())
}

// ShowOptions is the JSON options contract of helm_show. Keys are ABI:
// additive only.
type ShowOptions struct {
	Format string `json:"format"` // "all" (default), "chart", "values", "readme", "crds"
	Devel  bool   `json:"devel"`
	ChartRefOptions
}

// ParseShowOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseShowOptions(optsJSON string) (ShowOptions, error) {
	return decodeOptions[ShowOptions](optsJSON, "show")
}

// ShowChart is `helm show`: the chart definition, default values, README or
// CRDs of a chart reference — local path, repo chart (chart_repo_url) or
// oci:// reference — as the SDK renders them (YAML/text), without
// installing. clientObj is an optional registry client for private OCI.
func ShowChart(clientObj any, chartRef string, opts ShowOptions) (string, error) {
	format, err := showFormat(opts.Format)
	if err != nil {
		return "", err
	}
	client, err := resolveClient(clientObj, registry.IsOCI(chartRef), opts.PlainHTTP)
	if err != nil {
		return "", err
	}
	path, cleanup, err := localChartPath(client, chartRef, opts.ChartRefOptions)
	if err != nil {
		return "", err
	}
	defer cleanup()

	s := action.NewShow(format, action.NewConfiguration())
	s.Devel = opts.Devel
	s.SetRegistryClient(client)
	out, err := s.Run(path)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeChartLoad, err)
	}
	return out, nil
}

// showFormat maps the documented format names onto the SDK's.
func showFormat(s string) (action.ShowOutputFormat, error) {
	switch s {
	case "", "all":
		return action.ShowAll, nil
	case "chart", "values", "readme", "crds":
		return action.ShowOutputFormat(s), nil
	}
	return "", cerrors.New(cerrors.CodeInvalidArg,
		fmt.Sprintf(`invalid show format %q: must be "all", "chart", "values", "readme" or "crds"`, s))
}

// localChartPath returns a path on disk for chartRef: the reference itself
// when it is local, otherwise a copy pulled through the private, silent
// download path into a scratch directory the returned cleanup removes.
func localChartPath(client *registry.Client, chartRef string, opts ChartRefOptions) (string, func(), error) {
	if _, err := os.Stat(chartRef); err == nil {
		return chartRef, func() { /* nothing was materialised for a local path */ }, nil
	}
	cfg := &Config{Cfg: action.NewConfiguration(), Namespace: "default"}
	cfg.Cfg.RegistryClient = client
	dir, err := privateTempDir("show")
	if err != nil {
		return "", nil, err
	}
	rc, err := pullChartRef(cfg, chartRef, opts)
	if err != nil {
		removeBestEffort(dir)
		return "", nil, err
	}
	// pullChartRef loaded and discarded its scratch dir; re-save the chart
	// so the SDK's Show can read it from disk.
	path, err := chartutil.Save(rc.chart, dir)
	if err != nil {
		removeBestEffort(dir)
		return "", nil, cerrors.WithCode(cerrors.CodeIO, err)
	}
	return path, func() { removeBestEffort(dir) }, nil
}

// chartAccessor is the SDK's version-agnostic view of a chart.
func chartAccessor(c *chart.Chart) (chartpkg.Accessor, error) {
	ac, err := chartpkg.NewAccessor(c)
	if err != nil {
		return nil, cerrors.WithCode(cerrors.CodeChartInvalid, err)
	}
	return ac, nil
}
