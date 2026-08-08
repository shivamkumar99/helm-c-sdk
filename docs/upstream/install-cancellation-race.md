# DRAFT upstream issue for github.com/helm/helm — NOT yet submitted

> Status: awaiting project-owner review. Nothing has been posted anywhere.
> Repro module: `scratchpad/helm-race-repro` (standalone; the helm repo itself was not modified).

---

**Title:** Data race in `Install.RunWithContext`: detached install goroutine and `failRelease` both write `Release` status when the context is cancelled

**Labels (suggested):** bug, area/sdk

## Summary

`action.(*Install).RunWithContext` has a data race, flagged by the Go race detector, when
its context is cancelled: `performInstallCtx` unconditionally spawns the install goroutine,
and on `ctx.Done()` it returns the in-flight `*release.Release` to the caller while that
goroutine is still running. `RunWithContext` then calls `failRelease`, which writes the
release status via `rel.SetStatus(...)` — concurrently with the detached goroutine's own
`SetStatus` call inside `performInstall`. Two unsynchronized writes to the same `Release`.

The easiest deterministic trigger is a context that is **already cancelled** at call time,
but the window exists for any cancellation that fires while `performInstall` is running —
and `Upgrade.RunWithContext` uses the same detach-and-return pattern
(`releasingUpgrade`/`handleContext`), so it likely has the same shape (see also #12877 and
#13637, which describe the user-visible symptoms of the release status being clobbered on
cancellation, e.g. a release stuck in `pending-upgrade`).

## Affected versions

- **v4.2.3** (module `helm.sh/helm/v4`) — reproduced
- **main** (commit `e8a24895d`, 2026-08-06) — reproduced; the recent `goroutineCount`
  addition (#32328) tracks the leaked goroutine for tests but does not synchronize the
  shared `Release`

## Racing code

`pkg/action/install.go` (line numbers from v4.2.3; main is structurally identical):

```go
rel, err = i.performInstallCtx(ctx, rel, toBeAdopted, resources)   // :472
if err != nil {
    rel, err = i.failRelease(rel, err)                             // :474 → SetStatus (:582)
}
```

```go
func (i *Install) performInstallCtx(ctx context.Context, rel *release.Release, ...) (...) {
    resultChan := make(chan Msg, 1)
    go func() {                                                    // :486
        rel, err := i.performInstall(rel, toBeAdopted, resources)  // → SetStatus (:564)
        resultChan <- Msg{rel, err}
    }()
    select {
    case <-ctx.Done():
        return rel, ctx.Err()   // returns while the goroutine still owns rel
    case msg := <-resultChan:
        return msg.r, msg.e
    }
}
```

Both writers land in `pkg/release/v1.(*Release).SetStatus`. Beyond the detector report,
the practical consequence is a last-writer-wins scramble between `failed` (from
`failRelease`) and whatever the detached goroutine sets — the stored release status after
a cancelled install is nondeterministic.

## Minimal reproduction

```go
package repro

import (
	"context"
	"io"
	"testing"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/common"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	kubefake "helm.sh/helm/v4/pkg/kube/fake"
	"helm.sh/helm/v4/pkg/storage"
	"helm.sh/helm/v4/pkg/storage/driver"
)

func TestInstallRunWithContextCancelledRace(t *testing.T) {
	cfg := action.NewConfiguration()
	cfg.Releases = storage.Init(driver.NewMemory())
	cfg.KubeClient = &kubefake.FailingKubeClient{
		PrintingKubeClient: kubefake.PrintingKubeClient{Out: io.Discard},
	}
	cfg.Capabilities = common.DefaultCapabilities

	ch := &chart.Chart{
		Metadata: &chart.Metadata{APIVersion: "v2", Name: "racechart", Version: "0.1.0"},
		Templates: []*common.File{{
			Name: "templates/cm.yaml",
			Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm\n"),
		}},
	}

	inst := action.NewInstall(cfg)
	inst.ReleaseName = "race-rel"
	inst.Namespace = "default"

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before the call

	_, _ = inst.RunWithContext(ctx, ch, nil) // context.Canceled expected; the race is the bug
}
```

```
go test -race -count=20 .
```

## Race detector output (v4.2.3)

```
WARNING: DATA RACE
Write at 0x00c0002fec48 by goroutine 20:
  helm.sh/helm/v4/pkg/release/v1.(*Release).SetStatus()
      helm.sh/helm/v4@v4.2.3/pkg/release/v1/release.go:59
  helm.sh/helm/v4/pkg/action.(*Install).performInstall()
      helm.sh/helm/v4@v4.2.3/pkg/action/install.go:564
  helm.sh/helm/v4/pkg/action.(*Install).performInstallCtx.func1()
      helm.sh/helm/v4@v4.2.3/pkg/action/install.go:488

Previous write at 0x00c0002fec48 by goroutine 19:
  helm.sh/helm/v4/pkg/release/v1.(*Release).SetStatus()
      helm.sh/helm/v4@v4.2.3/pkg/release/v1/release.go:59
  helm.sh/helm/v4/pkg/action.(*Install).failRelease()
      helm.sh/helm/v4@v4.2.3/pkg/action/install.go:582
  helm.sh/helm/v4/pkg/action.(*Install).RunWithContext()
      helm.sh/helm/v4@v4.2.3/pkg/action/install.go:474
  helm-race-repro.TestInstallRunWithContextCancelledRace()
```

The identical report reproduces against `main` (`pkg/release/v1/release.go:61`,
`pkg/action/install.go:488/:564/:582`).

## Possible directions (not prescriptive)

- Short-circuit before spawning: check `ctx.Err()` at the top of `performInstallCtx` (fixes
  the pre-cancelled case only).
- On `ctx.Done()`, have the caller path operate on a **copy** of the release (or return
  only the error), leaving the detached goroutine sole owner of the original — the
  documented "install continues in the background" semantics then stay intact without a
  shared mutable struct.
- Or synchronize `Release.SetStatus`/`Info` mutation, though that spreads locking into a
  data type that is otherwise treated as plain data.

Happy to turn the repro into a PR-ready regression test if a maintainer indicates a
preferred fix direction.

---

*Found while building a C wrapper over the SDK; the wrapper works around it by refusing to
enter `RunWithContext` with an already-cancelled context, which silences the deterministic
trigger but not mid-flight cancellation.*
