package wrapper

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

func TestValidateReleaseName(t *testing.T) {
	tests := []struct {
		name    string
		release string
		wantOK  bool
	}{
		{name: "simple valid", release: "my-release", wantOK: true},
		{name: "with digits", release: "release-123", wantOK: true},
		{name: "empty", release: "", wantOK: false},
		{name: "uppercase", release: "MyRelease", wantOK: false},
		{name: "underscore", release: "my_release", wantOK: false},
		{name: "too long", release: strings.Repeat("a", 54), wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateReleaseName(tt.release)
			if tt.wantOK {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
			assert.Equal(t, cerrors.CodeInvalidArg, cerrors.FromError(err),
				"validation failures must map to HELM_ERR_INVALID_ARG")
		})
	}
}
