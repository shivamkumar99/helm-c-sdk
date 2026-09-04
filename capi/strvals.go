package main

/*
#include <stdint.h>
*/
import "C"

import (
	"github.com/shivamkumar99/helm-c-sdk/pkg/wrapper"
)

// helm_strvals_parse parses a Helm --set expression ("a=1,b.c=two") into a
// JSON object written to *out (caller frees with helm_free_string).
//
//export helm_strvals_parse
func helm_strvals_parse(s *C.char, out **C.char, errOut **C.char) C.int32_t {
	return strvalsShim(s, out, errOut, wrapper.ParseSetString)
}
