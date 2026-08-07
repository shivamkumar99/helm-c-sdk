package wrapper

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

func TestParseSetString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantJSON string
		wantCode cerrors.Code
	}{
		{name: "scalar and string", input: "a=1,b=two", wantJSON: `{"a":1,"b":"two"}`},
		{name: "nested key", input: "image.tag=v2", wantJSON: `{"image":{"tag":"v2"}}`},
		{name: "list", input: "ports={80,443}", wantJSON: `{"ports":[80,443]}`},
		{name: "malformed", input: "a=1,,=x=", wantCode: cerrors.CodeValues},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSetString(tt.input)
			if tt.wantCode != cerrors.CodeOK {
				assert.Equal(t, tt.wantCode, cerrors.FromError(err))
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tt.wantJSON, got)
		})
	}
}

func TestLoadChartFromDir(t *testing.T) {
	c, err := LoadChart(testChart)
	require.NoError(t, err)

	meta, err := ChartMetadataJSON(c)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(meta), &m))
	assert.Equal(t, "testchart", m["name"])
	assert.Equal(t, "0.1.0", m["version"])

	vals, err := ChartValuesJSON(c)
	require.NoError(t, err)
	assert.Contains(t, vals, "replicaCount")
}

func TestLoadChartMissingPath(t *testing.T) {
	_, err := LoadChart(filepath.Join(t.TempDir(), "nope"))
	assert.Equal(t, cerrors.CodeChartLoad, cerrors.FromError(err))
}

func TestAsChartWrongObject(t *testing.T) {
	_, err := AsChart("not a chart")
	assert.Equal(t, cerrors.CodeWrongHandleType, cerrors.FromError(err))
}

func TestSaveChartRoundTrip(t *testing.T) {
	c, err := LoadChart(testChart)
	require.NoError(t, err)

	dest := t.TempDir()
	tgz, err := SaveChart(c, dest)
	require.NoError(t, err)
	assert.FileExists(t, tgz)

	// The archive must load back as the same chart.
	reloaded, err := LoadChart(tgz)
	require.NoError(t, err)
	meta, err := ChartMetadataJSON(reloaded)
	require.NoError(t, err)
	assert.Contains(t, meta, `"name":"testchart"`)
}

func TestCreateChart(t *testing.T) {
	dir := t.TempDir()
	path, err := CreateChart("newchart", dir)
	require.NoError(t, err)
	assert.DirExists(t, path)
	assert.FileExists(t, filepath.Join(path, "Chart.yaml"))

	// The scaffold must itself be loadable.
	_, err = LoadChart(path)
	assert.NoError(t, err)
}

func TestCreateChartInvalidName(t *testing.T) {
	_, err := CreateChart("Bad Name!", t.TempDir())
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err))
}

func TestLintChart(t *testing.T) {
	report, err := LintChart(testChart, "")
	require.NoError(t, err)

	var r struct {
		TotalChartsLinted int `json:"total_charts_linted"`
		Messages          []struct {
			Severity int    `json:"severity"`
			Path     string `json:"path"`
			Error    string `json:"error"`
		} `json:"messages"`
		Errors []string `json:"errors"`
	}
	require.NoError(t, json.Unmarshal([]byte(report), &r))
	assert.Equal(t, 1, r.TotalChartsLinted)
	assert.Empty(t, r.Errors, "fixture chart should lint clean: %s", report)
}

func TestLintChartBadValuesJSON(t *testing.T) {
	_, err := LintChart(testChart, "{not json")
	assert.Equal(t, cerrors.CodeValues, cerrors.FromError(err))
}

func TestPackageChart(t *testing.T) {
	dest := t.TempDir()
	out, err := PackageChart(testChart, PackageOptions{Destination: dest})
	require.NoError(t, err)
	assert.FileExists(t, out)
	assert.Contains(t, filepath.Base(out), "testchart-0.1.0")
}

func TestPackageChartVersionOverride(t *testing.T) {
	dest := t.TempDir()
	out, err := PackageChart(testChart, PackageOptions{Destination: dest, Version: "9.9.9"})
	require.NoError(t, err)
	assert.Contains(t, filepath.Base(out), "9.9.9")
}

func TestPackageChartFailure(t *testing.T) {
	_, err := PackageChart(filepath.Join(t.TempDir(), "missing"), PackageOptions{})
	assert.Equal(t, cerrors.CodeChartInvalid, cerrors.FromError(err))
}

func TestParsePackageOptions(t *testing.T) {
	opts, err := ParsePackageOptions(`{"destination":"/tmp/x","version":"1.2.3"}`)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/x", opts.Destination)
	assert.Equal(t, "1.2.3", opts.Version)

	_, err = ParsePackageOptions(`{"unknown_key":true}`)
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err),
		"unknown option keys must fail loudly (docs/DESIGN.md §4)")

	_, err = ParsePackageOptions("")
	assert.NoError(t, err)
}
