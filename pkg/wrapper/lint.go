package wrapper

import (
	"helm.sh/helm/v4/pkg/action"
)

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
	vals, err := unmarshalValues(valuesJSON)
	if err != nil {
		return "", err
	}

	res := action.NewLint().Run([]string{path}, vals)
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
