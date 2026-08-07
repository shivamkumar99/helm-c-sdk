// Command genfixtures generates the signing fixtures the C harness consumes
// (signed chart + provenance + public keyring). Run by the Makefile and CI
// before the harness; nothing it produces is committed.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/shivamkumar99/helm-c-sdk/internal/testfixtures"
)

func main() {
	dir := flag.String("dir", "", "output directory for the signing fixtures")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "usage: genfixtures -dir <output-dir>")
		os.Exit(2)
	}
	if err := testfixtures.GenerateSigning(*dir); err != nil {
		fmt.Fprintf(os.Stderr, "generating fixtures: %v\n", err)
		os.Exit(1)
	}
}
