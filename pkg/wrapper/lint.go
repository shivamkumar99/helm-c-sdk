package wrapper

import (
	"helm.sh/helm/v4/pkg/action"
)

// LintOptions is the JSON options contract of helm_lint_run_opts — the full
// `helm lint` option set. Keys are ABI: additive only.
type LintOptions struct {
	Strict               bool   `json:"strict"`         // warnings are errors
	Namespace            string `json:"namespace"`      // .Release.Namespace during lint
	WithSubcharts        bool   `json:"with_subcharts"` // lint charts/ too
	Quiet                bool   `json:"quiet"`          // only warnings and errors
	SkipSchemaValidation bool   `json:"skip_schema_validation"`
	KubeVersion          string `json:"kube_version"` // e.g. "v1.30.0"
}

// ParseLintOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseLintOptions(optsJSON string) (LintOptions, error) {
	return decodeOptions[LintOptions](optsJSON, "lint")
}

// lintMessage and lintReport are the stable JSON shape returned to C callers;
// fields are additive-only (ABI).
type lintMessage struct {
	Severity int    `json:"severity"`
	Path     string `json:"path"`
	Error    string `json:"error"`
}

type lintReport struct {
	TotalChartsLinted int           `json:"total_charts_linted"`
	Messages          []lintMessage `json:"messages"`
	Errors            []string      `json:"errors"`
}

// LintChart lints the chart at path with optional values JSON overrides and
// returns a JSON report. Lint findings are data, not an error — the call only
// fails on malformed input.
func LintChart(path, valuesJSON string) (string, error) {
	return LintChartWithOptions(path, valuesJSON, LintOptions{})
}

// LintChartWithOptions is LintChart with the full `helm lint` option set.
func LintChartWithOptions(path, valuesJSON string, opts LintOptions) (string, error) {
	vals, err := unmarshalValues(valuesJSON)
	if err != nil {
		return "", err
	}
	kv, err := parseKubeVersion(opts.KubeVersion)
	if err != nil {
		return "", err
	}

	l := action.NewLint()
	l.Strict = opts.Strict
	l.Namespace = opts.Namespace
	l.WithSubcharts = opts.WithSubcharts
	l.Quiet = opts.Quiet
	l.SkipSchemaValidation = opts.SkipSchemaValidation
	l.KubeVersion = kv

	res := l.Run([]string{path}, vals)
	report := lintReport{
		TotalChartsLinted: res.TotalChartsLinted,
		Messages:          make([]lintMessage, 0, len(res.Messages)),
		Errors:            make([]string, 0, len(res.Errors)),
	}
	for _, m := range res.Messages {
		msg := lintMessage{Severity: m.Severity, Path: m.Path}
		if m.Err != nil {
			msg.Error = m.Err.Error()
		}
		report.Messages = append(report.Messages, msg)
	}
	for _, e := range res.Errors {
		report.Errors = append(report.Errors, e.Error())
	}
	return marshalJSON(report)
}
