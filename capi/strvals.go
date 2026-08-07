package main

/*
#include <stdint.h>
*/
import "C"

import (
	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
	"github.com/shivamkumar99/helm-c-sdk/internal/wrapper"
)

// helm_strvals_parse parses a Helm --set expression ("a=1,b.c=two") into a
// JSON object written to *out (caller frees with helm_free_string).
//
//export helm_strvals_parse
func helm_strvals_parse(s *C.char, out **C.char, errOut **C.char) (code C.int32_t) {
	clearErrorOut(errOut)
	defer recoverToCode(&code, errOut)
	if s == nil || out == nil {
		return failure(errOut, cerrors.New(cerrors.CodeInvalidArg, "s and out must not be NULL"))
	}
	jsonStr, err := wrapper.ParseSetString(C.GoString(s))
	if err != nil {
		return failure(errOut, err)
	}
	*out = C.CString(jsonStr)
	return C.int32_t(cerrors.CodeOK)
}
