package wrapper

import (
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// MergeChartValues coalesces the chart's default values with the caller's
// overrides JSON (overrides win) and returns the effective values as JSON —
// the same composition an install would use.
func MergeChartValues(chartObj any, valuesJSON string) (string, error) {
	c, err := AsChart(chartObj)
	if err != nil {
		return "", err
	}
	vals, err := unmarshalValues(valuesJSON)
	if err != nil {
		return "", err
	}
	merged, err := commonutil.CoalesceValues(c, vals)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}
	return marshalJSON(merged)
}

// ValidateValuesSchema checks the effective values (chart defaults + caller
// overrides) against the chart's values.schema.json, if it ships one. A chart
// without a schema always passes.
func ValidateValuesSchema(chartObj any, valuesJSON string) error {
	c, err := AsChart(chartObj)
	if err != nil {
		return err
	}
	vals, err := unmarshalValues(valuesJSON)
	if err != nil {
		return err
	}
	merged, err := commonutil.CoalesceValues(c, vals)
	if err != nil {
		return cerrors.WithCode(cerrors.CodeValues, err)
	}
	return cerrors.WithCode(cerrors.CodeValues, commonutil.ValidateAgainstSchema(c, merged))
}
