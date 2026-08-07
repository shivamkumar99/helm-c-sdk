package cerrors

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Code
	}{
		{name: "nil is OK", err: nil, want: CodeOK},
		{name: "coded error", err: New(CodeInvalidHandle, "boom"), want: CodeInvalidHandle},
		{name: "wrapped coded error", err: fmt.Errorf("outer: %w", New(CodeNotFound, "gone")), want: CodeNotFound},
		{name: "WithCode wrapping", err: WithCode(CodeChartLoad, errors.New("bad chart")), want: CodeChartLoad},
		{name: "context cancelled", err: context.Canceled, want: CodeCancelled},
		{name: "context deadline", err: context.DeadlineExceeded, want: CodeCancelled},
		{name: "plain error", err: errors.New("mystery"), want: CodeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FromError(tt.err))
		})
	}
}

func TestWithCodeNil(t *testing.T) {
	assert.NoError(t, WithCode(CodeChartLoad, nil))
}

func TestCodedErrorMessageAndUnwrap(t *testing.T) {
	inner := errors.New("inner detail")
	err := WithCode(CodeValues, inner)
	assert.EqualError(t, err, "inner detail")
	assert.ErrorIs(t, err, inner)
}
