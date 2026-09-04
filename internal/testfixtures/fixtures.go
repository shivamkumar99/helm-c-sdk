// Package testfixtures generates every test fixture at runtime — charts,
// kubeconfig, and PGP signing material — so the repository carries no
// committed test data and each suite is self-contained.
package testfixtures

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ProtonMail/go-crypto/openpgp"

	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"helm.sh/helm/v4/pkg/provenance"
)

// KubeconfigYAML is a syntactically valid kubeconfig pointing at an
// unreachable server — config construction succeeds, actions fail cleanly.
const KubeconfigYAML = `apiVersion: v1
kind: Config
clusters:
  - name: helmc-test
    cluster:
      server: https://127.0.0.1:1
contexts:
  - name: helmc-test
    context:
      cluster: helmc-test
      user: helmc-test
current-context: helmc-test
users:
  - name: helmc-test
    user: {}
`

const testChartYAML = `apiVersion: v2
name: testchart
description: Generated fixture chart
type: application
version: 0.1.0
appVersion: "1.0.0"
`

const testChartValues = `replicaCount: 1
image:
  repository: nginx
  tag: stable
`

const testChartTemplate = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-config
data:
  replicas: {{ .Values.replicaCount | quote }}
  image: {{ .Values.image.repository }}:{{ .Values.image.tag }}
`

const schemaChartYAML = `apiVersion: v2
name: schemachart
description: Generated fixture chart with a values schema
type: application
version: 0.1.0
appVersion: "1.0.0"
`

const schemaChartSchema = `{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "replicaCount": {
      "type": "integer",
      "minimum": 0
    }
  },
  "required": ["replicaCount"]
}
`

const schemaChartTemplate = `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-schema-config
data:
  replicas: {{ .Values.replicaCount | quote }}
`

// SignedChartBase is the <name>-<version> stem of the signing fixture.
const SignedChartBase = "testchart-0.1.0"

func writeFiles(dir string, files map[string]string) error {
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return err
		}
	}
	return nil
}

// WriteTestChart writes the standard fixture chart under dir and returns its
// chart directory.
func WriteTestChart(dir string) (string, error) {
	chartDir := filepath.Join(dir, "testchart")
	return chartDir, writeFiles(chartDir, map[string]string{
		"Chart.yaml":               testChartYAML,
		"values.yaml":              testChartValues,
		"templates/configmap.yaml": testChartTemplate,
	})
}

// WriteSchemaChart writes the schema-validating fixture chart under dir.
func WriteSchemaChart(dir string) (string, error) {
	chartDir := filepath.Join(dir, "schemachart")
	return chartDir, writeFiles(chartDir, map[string]string{
		"Chart.yaml":               schemaChartYAML,
		"values.yaml":              "replicaCount: 1\n",
		"values.schema.json":       schemaChartSchema,
		"templates/configmap.yaml": schemaChartTemplate,
	})
}

// WriteKubeconfig writes the unreachable-cluster kubeconfig under dir.
func WriteKubeconfig(dir string) (string, error) {
	path := filepath.Join(dir, "kubeconfig.yaml")
	return path, os.WriteFile(path, []byte(KubeconfigYAML), 0o600)
}

// packageFixtureChart authors and packages the test chart under dir and
// returns the .tgz path.
func packageFixtureChart(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	chartDir, err := WriteTestChart(dir)
	if err != nil {
		return "", err
	}
	c, err := loader.Load(chartDir)
	if err != nil {
		return "", err
	}
	tgz, err := chartutil.Save(c, dir)
	if err != nil {
		return "", fmt.Errorf("packaging signing fixture: %w", err)
	}
	return tgz, nil
}

// signFixtureArchive clear-signs tgz with a throwaway key, writing
// <tgz>.prov and <dir>/pubring.gpg.
func signFixtureArchive(dir, tgz string) error {
	entity, err := openpgp.NewEntity("helm-c-sdk-test", "throwaway test-only key", "test@invalid", nil)
	if err != nil {
		return fmt.Errorf("generating test key: %w", err)
	}

	archive, err := os.ReadFile(tgz) // #nosec G304 -- test-fixture generator; tgz was just written by chartutil.Save under caller-owned dir
	if err != nil {
		return err
	}
	sig := &provenance.Signatory{Entity: entity}
	block, err := sig.ClearSign(archive, filepath.Base(tgz), []byte(testChartYAML))
	if err != nil {
		return fmt.Errorf("signing fixture: %w", err)
	}
	// #nosec G306 G703 -- test-fixture generator; path derives from the tgz created above
	if err := os.WriteFile(tgz+".prov", []byte(block), 0o600); err != nil {
		return err
	}

	var pub bytes.Buffer
	if err := entity.Serialize(&pub); err != nil {
		return fmt.Errorf("serializing public key: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pubring.gpg"), pub.Bytes(), 0o600); err != nil {
		return err
	}
	// The secret keyring lets suites exercise signing, not just verifying.
	var priv bytes.Buffer
	if err := entity.SerializePrivate(&priv, nil); err != nil {
		return fmt.Errorf("serializing private key: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "secring.gpg"), priv.Bytes(), 0o600)
}

// GenerateSigning creates a signed chart archive, its .prov provenance file,
// and both keyrings under dir (a throwaway PGP key generated per call):
// <dir>/testchart-0.1.0.tgz, .tgz.prov, pubring.gpg (verification) and
// secring.gpg (signing; identity "helm-c-sdk-test", no passphrase).
func GenerateSigning(dir string) error {
	tgz, err := packageFixtureChart(dir)
	if err != nil {
		return err
	}
	return signFixtureArchive(dir, tgz)
}
