package wrapper

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// decodeOptions strictly decodes a JSON options string into T; unknown keys
// fail loudly so a typo is never silently ignored (docs/DESIGN.md §4). Empty input
// returns the zero value (all defaults).
func decodeOptions[T any](optsJSON, what string) (T, error) {
	var opts T
	if optsJSON == "" {
		return opts, nil
	}
	dec := json.NewDecoder(strings.NewReader(optsJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&opts); err != nil {
		return opts, cerrors.WithCode(cerrors.CodeInvalidArg,
			fmt.Errorf("invalid %s options JSON: %w", what, err))
	}
	return opts, nil
}

// marshalJSON renders v as the JSON that crosses the C boundary.
func marshalJSON(v any) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeUnknown, fmt.Errorf("encoding result: %w", err))
	}
	return string(data), nil
}

// unmarshalValues parses caller-supplied values JSON. Empty input means no
// overrides; malformed input is a values error.
func unmarshalValues(valuesJSON string) (map[string]any, error) {
	if valuesJSON == "" {
		return nil, nil
	}
	var vals map[string]any
	if err := json.Unmarshal([]byte(valuesJSON), &vals); err != nil {
		return nil, cerrors.WithCode(cerrors.CodeValues, fmt.Errorf("invalid values JSON: %w", err))
	}
	return vals, nil
}
