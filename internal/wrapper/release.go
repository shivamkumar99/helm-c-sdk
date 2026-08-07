package wrapper

import (
	"time"

	chartpkg "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/release"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

// releaseSummary is the stable JSON shape for a release crossing the C
// boundary. Fields are additive-only (ABI).
type releaseSummary struct {
	Name         string            `json:"name"`
	Namespace    string            `json:"namespace"`
	Revision     int               `json:"revision"`
	Status       string            `json:"status"`
	Notes        string            `json:"notes,omitempty"`
	DeployedAt   string            `json:"deployed_at,omitempty"`
	ChartName    string            `json:"chart_name,omitempty"`
	ChartVersion string            `json:"chart_version,omitempty"`
	AppVersion   string            `json:"app_version,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	Manifest     string            `json:"manifest,omitempty"`
}

// summarizeRelease reads a generic release.Releaser through the SDK's
// accessor abstraction. Manifests are included only where documented (they
// can be large).
func summarizeRelease(rel release.Releaser, includeManifest bool) (releaseSummary, error) {
	acc, err := release.NewAccessor(rel)
	if err != nil {
		return releaseSummary{}, cerrors.WithCode(cerrors.CodeRelease, err)
	}
	s := releaseSummary{
		Name:      acc.Name(),
		Namespace: acc.Namespace(),
		Revision:  acc.Version(),
		Status:    acc.Status(),
		Notes:     acc.Notes(),
		Labels:    acc.Labels(),
	}
	if at := acc.DeployedAt(); !at.IsZero() {
		s.DeployedAt = at.Format(time.RFC3339)
	}
	if ch := acc.Chart(); ch != nil {
		if ca, err := chartpkg.NewAccessor(ch); err == nil {
			s.ChartName = ca.Name()
			// MetadataAsMap keys by Go field name (SDK structToMap), not
			// by the lowercase YAML/JSON names.
			meta := ca.MetadataAsMap()
			s.ChartVersion, _ = meta["Version"].(string)
			s.AppVersion, _ = meta["AppVersion"].(string)
		}
	}
	if includeManifest {
		s.Manifest = acc.Manifest()
	}
	return s, nil
}

// summarizeReleases maps a release list into summaries (no manifests).
func summarizeReleases(rels []release.Releaser) ([]releaseSummary, error) {
	out := make([]releaseSummary, 0, len(rels))
	for _, rel := range rels {
		s, err := summarizeRelease(rel, false)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
