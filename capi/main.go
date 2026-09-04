// Package main is the C boundary of helm-c, built with -buildmode=c-shared.
// It is the ONLY package that imports "C" (cgo types do not cross Go package
// boundaries). Every //export shim is thin: convert input → call
// internal/wrapper → convert output; all logic lives in internal packages.
package main

// main is required by -buildmode=c-shared; it never runs.
func main() { /* a c-shared library has no entry point; the linker only needs the symbol */ }
