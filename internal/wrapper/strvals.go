package wrapper

import (
	"helm.sh/helm/v4/pkg/strvals"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

// ParseSetString parses a Helm --set expression ("a=1,b.c=two") into a JSON
// object string.
func ParseSetString(s string) (string, error) {
	vals, err := strvals.Parse(s)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}
	return marshalJSON(vals)
}
