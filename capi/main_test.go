package main

import (
	"os"
	"testing"

	"github.com/shivamkumar99/helm-c-sdk/internal/testfixtures"
)

// Fixture locations, generated fresh for every test run — the repository
// carries no committed test data.
var (
	fixtureChart      string
	fixtureSchema     string
	fixtureKubeconfig string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "helm-c-capi-fixtures-*")
	if err != nil {
		panic(err)
	}
	if fixtureChart, err = testfixtures.WriteTestChart(dir); err != nil {
		panic(err)
	}
	if fixtureSchema, err = testfixtures.WriteSchemaChart(dir); err != nil {
		panic(err)
	}
	if fixtureKubeconfig, err = testfixtures.WriteKubeconfig(dir); err != nil {
		panic(err)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
