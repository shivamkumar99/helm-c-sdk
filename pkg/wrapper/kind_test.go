//go:build kind

package wrapper

// Real-cluster e2e, run by the kind CI job (`go test -tags kind`). Expects a
// reachable cluster via the default kubeconfig resolution (kind-action sets
// KUBECONFIG).

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKindReleaseLifecycle(t *testing.T) {
	cfg, err := NewConfig(ConfigOptions{Namespace: "default"})
	require.NoError(t, err)

	chart := loadFixtureChart(t)
	ctx := context.Background()

	out, err := InstallRelease(ctx, cfg, chart, "", "kind-rel", `{"replicaCount":2}`, InstallOptions{
		Wait:           "watcher",
		TimeoutSeconds: 120,
	})
	require.NoError(t, err)

	var rel summaryJSON
	require.NoError(t, json.Unmarshal([]byte(out), &rel))
	assert.Equal(t, "deployed", rel.Status)
	assert.Equal(t, 1, rel.Revision)

	out, err = UpgradeRelease(ctx, cfg, chart, "", "kind-rel", `{"replicaCount":3}`, UpgradeOptions{
		Wait:           "watcher",
		TimeoutSeconds: 120,
	})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &rel))
	assert.Equal(t, 2, rel.Revision)

	require.NoError(t, RollbackRelease(cfg, "kind-rel", RollbackOptions{Version: 1, TimeoutSeconds: 120}))

	out, err = StatusRelease(cfg, "kind-rel", StatusOptions{})
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(out), &rel))
	assert.Equal(t, 3, rel.Revision)

	uninstOut, err := UninstallRelease(cfg, "kind-rel", UninstallOptions{TimeoutSeconds: 120})
	require.NoError(t, err)
	assert.Contains(t, uninstOut, "uninstalled")
}
