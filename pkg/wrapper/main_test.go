package wrapper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shivamkumar99/helm-c-sdk/internal/testfixtures"
)

// Fixture locations, generated fresh for every test run — the repository
// carries no committed test data.
var (
	testChart      string
	schemaChart    string
	kubeconfigPath string
	signingDir     string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "helm-c-fixtures-*")
	if err != nil {
		panic(err)
	}
	if testChart, err = testfixtures.WriteTestChart(dir); err != nil {
		panic(err)
	}
	if schemaChart, err = testfixtures.WriteSchemaChart(dir); err != nil {
		panic(err)
	}
	if kubeconfigPath, err = testfixtures.WriteKubeconfig(dir); err != nil {
		panic(err)
	}
	signingDir = filepath.Join(dir, "signing")
	if err = testfixtures.GenerateSigning(signingDir); err != nil {
		panic(err)
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
