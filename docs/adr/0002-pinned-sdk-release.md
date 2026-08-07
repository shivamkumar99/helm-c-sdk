# ADR-0002: Pin helm.sh/helm/v4 to an exact release tag

- Status: **Accepted** (2026-08-07)

## Context
The wrapper must always be able to answer "which Helm SDK does this build contain?" —
for CVE response, for binding compatibility, and for reproducible builds. A `replace` to
the local clone (which tracks `main`, ahead of releases) would make builds irreproducible
and let unreleased APIs leak into the ABI.

## Decision
`go.mod` requires `helm.sh/helm/v4 v4.2.3` (newest stable at time of decision) with **no
replace directive and no branch tracking**. The local `../helm` clone is read-only
reference; API existence is verified against the pinned release (`go doc` resolves through
the module cache, which serves the pin). `helm_sdk_version()` exposes the pin at runtime
from Go build info.

## Consequences
- Reproducible builds; single source of truth for the dependency version.
- APIs on Helm `main` but absent from the pin are unusable until a pin bump — by design.
- Pin bumps are standalone changes run through the full three-OS gate.
