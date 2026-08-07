// Package cerrors defines the stable C error-code taxonomy and the mapping
// from Go errors to those codes. The codes are ABI (mirrored by
// helm_error_code in include/helm_c.h): never renumber, remove, or repurpose
// one — only append.
package cerrors

import (
	"context"
	"errors"
)

// Code is a stable C error code. Zero is success; every failure is negative.
type Code int32

const (
	CodeOK              Code = 0
	CodeUnknown         Code = -1
	CodeInvalidArg      Code = -2
	CodeInvalidHandle   Code = -3
	CodeWrongHandleType Code = -4
	CodePanic           Code = -5
	CodeCancelled       Code = -6
	CodeNotFound        Code = -7
	CodeIO              Code = -8

	CodeChartLoad    Code = -20
	CodeChartInvalid Code = -21
	CodeValues       Code = -22
	CodeRender       Code = -23

	CodeRegistry Code = -40
	CodeRepo     Code = -41

	CodeKube    Code = -60
	CodeStorage Code = -61
	CodeRelease Code = -62
)

// CodedError attaches a stable C error code to a Go error so it survives
// wrapping and can be recovered at the C boundary via FromError.
type CodedError struct {
	Code Code
	Err  error
}

func (e *CodedError) Error() string { return e.Err.Error() }

func (e *CodedError) Unwrap() error { return e.Err }

// New builds a CodedError from a message.
func New(code Code, msg string) error {
	return &CodedError{Code: code, Err: errors.New(msg)}
}

// WithCode wraps err with a stable code. A nil err stays nil.
func WithCode(code Code, err error) error {
	if err == nil {
		return nil
	}
	return &CodedError{Code: code, Err: err}
}

// FromError resolves the C code for err: the innermost CodedError wins, then
// well-known stdlib errors, then CodeUnknown. A nil err is CodeOK.
func FromError(err error) Code {
	if err == nil {
		return CodeOK
	}
	var coded *CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return CodeCancelled
	}
	return CodeUnknown
}
