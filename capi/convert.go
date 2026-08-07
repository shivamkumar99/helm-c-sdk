package main

/*
#include <stdint.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"unsafe"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

// cErrorOut writes err into errOut as a malloc'd C string the caller frees
// with helm_free_string. A nil errOut means the caller declined error detail.
func cErrorOut(errOut **C.char, err error) {
	if errOut == nil || err == nil {
		return
	}
	*errOut = C.CString(err.Error())
}

// clearErrorOut initializes an optional error out-param to NULL so callers
// can rely on "NULL unless the call failed".
func clearErrorOut(errOut **C.char) {
	if errOut != nil {
		*errOut = nil
	}
}

// recoverToCode converts a Go panic into CodePanic instead of letting it
// cross the C boundary (undefined behavior). Use in every //export shim:
//
//	defer recoverToCode(&code, errOut)
func recoverToCode(code *C.int32_t, errOut **C.char) {
	if r := recover(); r != nil {
		*code = C.int32_t(cerrors.CodePanic)
		if errOut != nil {
			*errOut = C.CString(fmt.Sprintf("helm-c: recovered panic: %v", r))
		}
	}
}

// optionalGoString converts a nullable C string; NULL means "not provided".
func optionalGoString(s *C.char) string {
	if s == nil {
		return ""
	}
	return C.GoString(s)
}

// freeCString releases a string previously allocated by C.CString. NULL-safe.
func freeCString(s *C.char) {
	if s != nil {
		C.free(unsafe.Pointer(s))
	}
}

// failure maps err to its C error code and fills errOut in one step.
func failure(errOut **C.char, err error) C.int32_t {
	cErrorOut(errOut, err)
	return C.int32_t(cerrors.FromError(err))
}
