package wrapper

import (
	"log/slog"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/downloader"
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
	CaFile                string `json:"ca_file"`
	CertFile              string `json:"cert_file"`
	KeyFile               string `json:"key_file"`
	PassCredentialsAll    bool   `json:"pass_credentials_all"`
	Verify                bool   `json:"verify"`       // verify the provenance signature
	VerifyLater           bool   `json:"verify_later"` // fetch the .prov file, verify separately
	Keyring               string `json:"keyring"`
	Devel                 bool   `json:"devel"` // allow development (pre-release) versions
}

// ParsePullOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParsePullOptions(optsJSON string) (PullOptions, error) {
	return decodeOptions[PullOptions](optsJSON, "pull")
}

// defaultClientFor is the one rule for registry clients across pull, push,
// dependency resolution and chart-ref installs: use the caller's client
// when there is one, otherwise build an anonymous one honouring plainHTTP —
// but only when the operation actually talks to an OCI registry.
func defaultClientFor(existing *registry.Client, needed, plainHTTP bool) (*registry.Client, error) {
	if existing != nil {
		return existing, nil
	}
	if !needed {
		return nil, nil
	}
	return NewRegistryClient(RegistryClientOptions{PlainHTTP: plainHTTP})
}

// resolveClient applies defaultClientFor to an optional registry object.
func resolveClient(clientObj any, needed bool, plainHTTP bool) (*registry.Client, error) {
	var existing *registry.Client
	if clientObj != nil {
		c, err := AsRegistryClient(clientObj)
		if err != nil {
			return nil, err
		}
		existing = c
	}
	return defaultClientFor(existing, needed, plainHTTP)
}

// PullChart downloads a chart from an HTTP repo or an oci:// reference into
// opts.DestDir. clientObj is optional (nil = default client for OCI refs).
// Repository caches live in a private scratch directory that is removed
// afterwards; the host user's helm caches are never touched.
func PullChart(clientObj any, chartRef string, opts PullOptions) (string, error) {
	client, err := resolveClient(clientObj, registry.IsOCI(chartRef), opts.PlainHTTP)
	if err != nil {
		return "", err
	}
	scratch, err := privateTempDir("pull")
	if err != nil {
		return "", err
	}
	defer removeBestEffort(scratch)

	p := action.NewPull(action.WithConfig(action.NewConfiguration()))
	p.Settings = settingsInDir(scratch)
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
	p.CaFile = opts.CaFile
	p.CertFile = opts.CertFile
	p.KeyFile = opts.KeyFile
	p.PassCredentialsAll = opts.PassCredentialsAll
	p.Verify = opts.Verify
	p.VerifyLater = opts.VerifyLater
	p.Keyring = opts.Keyring
	p.Devel = opts.Devel

	out, err := p.Run(chartRef)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRepo, err)
	}
	return marshalJSON(map[string]string{"output": out})
}

// PushOptions is the JSON options contract of helm_push. Keys are ABI:
// additive only.
type PushOptions struct {
	PlainHTTP             bool   `json:"plain_http"`
	InsecureSkipTLSVerify bool   `json:"insecure_skip_tls_verify"`
	CertFile              string `json:"cert_file"`
	KeyFile               string `json:"key_file"`
	CaFile                string `json:"ca_file"`
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
		action.WithTLSClientConfig(opts.CertFile, opts.KeyFile, opts.CaFile),
		action.WithPushOptWriter(LogWriter(slog.LevelInfo)),
	)
	p.Settings = newSettings()

	out, err := p.Run(chartPath, remote)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRegistry, err)
	}
	return marshalJSON(map[string]string{"output": out})
}

// verificationStrategy maps the documented verify option strings onto the
// SDK's strategies. "" and "never" skip verification; the legacy bool form
// (true) means "always".
func verificationStrategy(s string) (downloader.VerificationStrategy, error) {
	switch s {
	case "", "never", "false":
		return downloader.VerifyNever, nil
	case "always", "true":
		return downloader.VerifyAlways, nil
	case "if_possible":
		return downloader.VerifyIfPossible, nil
	case "later":
		return downloader.VerifyLater, nil
	}
	return downloader.VerifyNever, cerrors.New(cerrors.CodeInvalidArg,
		`invalid verify value: must be "never", "always", "if_possible" or "later"`)
}
