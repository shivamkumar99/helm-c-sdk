package wrapper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

const schemaChart = "../../testdata/schemachart"

func TestMergeChartValues(t *testing.T) {
	c, err := LoadChart(testChart)
	require.NoError(t, err)

	tests := []struct {
		name      string
		overrides string
		check     func(t *testing.T, merged map[string]any)
	}{
		{
			name:      "no overrides keeps defaults",
			overrides: "",
			check: func(t *testing.T, m map[string]any) {
				assert.EqualValues(t, 1, m["replicaCount"])
			},
		},
		{
			name:      "override wins",
			overrides: `{"replicaCount":5}`,
			check: func(t *testing.T, m map[string]any) {
				assert.EqualValues(t, 5, m["replicaCount"])
			},
		},
		{
			name:      "nested merge preserves siblings",
			overrides: `{"image":{"tag":"edge"}}`,
			check: func(t *testing.T, m map[string]any) {
				img := m["image"].(map[string]any)
				assert.Equal(t, "edge", img["tag"])
				assert.Equal(t, "nginx", img["repository"], "unrelated nested keys survive")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := MergeChartValues(c, tt.overrides)
			require.NoError(t, err)
			var m map[string]any
			require.NoError(t, json.Unmarshal([]byte(out), &m))
			tt.check(t, m)
		})
	}
}

func TestMergeChartValuesBadJSON(t *testing.T) {
	c, err := LoadChart(testChart)
	require.NoError(t, err)
	_, err = MergeChartValues(c, "{broken")
	assert.Equal(t, cerrors.CodeValues, cerrors.FromError(err))
}

func TestValidateValuesSchema(t *testing.T) {
	c, err := LoadChart(schemaChart)
	require.NoError(t, err)

	assert.NoError(t, ValidateValuesSchema(c, ""), "defaults satisfy the schema")
	assert.NoError(t, ValidateValuesSchema(c, `{"replicaCount":3}`))

	err = ValidateValuesSchema(c, `{"replicaCount":-5}`)
	assert.Equal(t, cerrors.CodeValues, cerrors.FromError(err),
		"schema violation maps to HELM_ERR_VALUES")

	err = ValidateValuesSchema(c, `{"replicaCount":"three"}`)
	assert.Equal(t, cerrors.CodeValues, cerrors.FromError(err))
}

func TestValidateValuesSchemaNoSchemaAlwaysPasses(t *testing.T) {
	c, err := LoadChart(testChart) // testchart ships no values.schema.json
	require.NoError(t, err)
	assert.NoError(t, ValidateValuesSchema(c, `{"anything":"goes"}`))
}

func TestParseRenderOptions(t *testing.T) {
	opts, err := ParseRenderOptions("")
	require.NoError(t, err)
	assert.Equal(t, "release-name", opts.Name)
	assert.Equal(t, "default", opts.Namespace)
	assert.Equal(t, 1, opts.Revision)
	assert.True(t, opts.IsInstall)

	opts, err = ParseRenderOptions(`{"name":"myrel","namespace":"prod","revision":7,"is_upgrade":true}`)
	require.NoError(t, err)
	assert.Equal(t, "myrel", opts.Name)
	assert.Equal(t, "prod", opts.Namespace)
	assert.Equal(t, 7, opts.Revision)
	assert.True(t, opts.IsUpgrade)
	assert.False(t, opts.IsInstall, "explicit is_upgrade suppresses the is_install default")

	_, err = ParseRenderOptions(`{"unknown":"key"}`)
	assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err),
		"unknown option keys must fail loudly (ADR-0004)")
}

func TestRenderChart(t *testing.T) {
	c, err := LoadChart(testChart)
	require.NoError(t, err)

	opts, err := ParseRenderOptions(`{"name":"myrel"}`)
	require.NoError(t, err)
	out, err := RenderChart(c, `{"replicaCount":3}`, opts)
	require.NoError(t, err)

	var manifests map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &manifests))
	manifest, ok := manifests["testchart/templates/configmap.yaml"]
	require.True(t, ok, "rendered manifest keyed by template path, got keys: %v", manifests)
	assert.Contains(t, manifest, "myrel-config", "release name substituted")
	assert.Contains(t, manifest, `replicas: "3"`, "override value rendered")
	assert.Contains(t, manifest, "nginx:stable", "default values rendered")
}

func TestRenderChartBadTemplate(t *testing.T) {
	dir := t.TempDir()
	path, err := CreateChart("badchart", dir)
	require.NoError(t, err)

	// Sabotage a template so rendering fails.
	broken := filepath.Join(path, "templates", "broken.yaml")
	require.NoError(t, os.WriteFile(broken, []byte("{{ fail \"boom\" }}\n"), 0o644))

	c, err := LoadChart(path)
	require.NoError(t, err)
	opts, err := ParseRenderOptions("")
	require.NoError(t, err)
	_, err = RenderChart(c, "", opts)
	assert.Equal(t, cerrors.CodeRender, cerrors.FromError(err))
}
