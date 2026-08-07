package wrapper

import (
	"log/slog"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/registry"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// PullOptions is the JSON options contract of helm_pull. Keys are ABI:
// additive only.
type PullOptions struct {
	DestDir               string `json:"dest_dir"`
	Version               string `json:"version"`
	RepoURL               string `json:"repo_url"`
	Untar                 bool   `json:"untar"`
	UntarDir              string `json:"untar_dir"`
	PlainHTTP             bool   `json:"plain_http"`
	InsecureSkipTLSVerify bool   `json:"insecure_skip_tls_verify"`
	Username              string `json:"username"`
	Password              string `json:"password"`
}

// ParsePullOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParsePullOptions(optsJSON string) (PullOptions, error) {
	return decodeOptions[PullOptions](optsJSON, "pull")
}

// resolveClient returns the caller's registry client, or — for OCI refs with
// no client supplied — a default one honoring plainHTTP.
func resolveClient(clientObj any, needed bool, plainHTTP bool) (*registry.Client, error) {
	if clientObj != nil {
		return AsRegistryClient(clientObj)
	}
	if !needed {
		return nil, nil
	}
	return NewRegistryClient(RegistryClientOptions{PlainHTTP: plainHTTP})
}

// PullChart downloads a chart from an HTTP repo or an oci:// reference into
// opts.DestDir. clientObj is optional (nil = default client for OCI refs).
func PullChart(clientObj any, chartRef string, opts PullOptions) (string, error) {
	client, err := resolveClient(clientObj, registry.IsOCI(chartRef), opts.PlainHTTP)
	if err != nil {
		return "", err
	}

	p := action.NewPull(action.WithConfig(action.NewConfiguration()))
	p.Settings = cli.New()
	p.SetRegistryClient(client)
	p.DestDir = opts.DestDir
	if p.DestDir == "" {
		p.DestDir = "."
	}
	p.Version = opts.Version
	p.RepoURL = opts.RepoURL
	p.Untar = opts.Untar
	p.UntarDir = opts.UntarDir
	p.PlainHTTP = opts.PlainHTTP
	p.InsecureSkipTLSVerify = opts.InsecureSkipTLSVerify
	p.Username = opts.Username
	p.Password = opts.Password

	out, err := p.Run(chartRef)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRepo, err)
	}
	return marshalJSON(map[string]string{"output": out})
}

// PushOptions is the JSON options contract of helm_push. Keys are ABI:
// additive only.
type PushOptions struct {
	PlainHTTP             bool `json:"plain_http"`
	InsecureSkipTLSVerify bool `json:"insecure_skip_tls_verify"`
}

// ParsePushOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParsePushOptions(optsJSON string) (PushOptions, error) {
	return decodeOptions[PushOptions](optsJSON, "push")
}

// PushChart uploads the chart archive at chartPath to an oci:// remote.
// clientObj is optional (nil = default client honoring plain_http).
func PushChart(clientObj any, chartPath, remote string, opts PushOptions) (string, error) {
	client, err := resolveClient(clientObj, true, opts.PlainHTTP)
	if err != nil {
		return "", err
	}

	cfg := action.NewConfiguration()
	cfg.RegistryClient = client
	p := action.NewPushWithOpts(
		action.WithPushConfig(cfg),
		action.WithPlainHTTP(opts.PlainHTTP),
		action.WithInsecureSkipTLSVerify(opts.InsecureSkipTLSVerify),
		action.WithPushOptWriter(LogWriter(slog.LevelInfo)),
	)
	p.Settings = cli.New()

	out, err := p.Run(chartPath, remote)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRegistry, err)
	}
	return marshalJSON(map[string]string{"output": out})
}
