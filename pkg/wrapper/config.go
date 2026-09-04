package wrapper

import (
	"log/slog"
	"os"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/cli"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// Config bundles an initialized action.Configuration with its target
// namespace. One Config supports parallel actions on different releases;
// concurrent writes to the same release can corrupt its history (SDK
// semantics) — bindings must serialize per release.
type Config struct {
	Cfg       *action.Configuration
	Namespace string

	// opts is what the config was built from, kept so an action can derive a
	// sibling configuration (another namespace, or a throwaway for a
	// client-side dry run) without touching the shared one.
	opts ConfigOptions

	// tempKubeconfig holds the private file backing kubeconfig_content; it
	// must outlive the config (loading is lazy) and is removed by Close.
	tempKubeconfig string
}

// removeBestEffort deletes a temp path; cleanup must never fail an
// operation, so unexpected failures surface through the log handler only.
func removeBestEffort(path string) {
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		slog.New(CurrentLogHandler()).Debug("temp cleanup failed", "path", path, "error", err)
	}
}

// Close releases resources owned by the config. Idempotent.
func (c *Config) Close() {
	if c.tempKubeconfig != "" {
		removeBestEffort(c.tempKubeconfig)
		c.tempKubeconfig = ""
	}
}

// CloseConfig closes a registry object holding a Config; used by
// helm_config_free after the handle is released.
func CloseConfig(obj any) {
	if c, ok := obj.(*Config); ok {
		c.Close()
	}
}

// ConfigOptions is the JSON options contract of helm_config_new. It exposes
// the SDK's full kube connection surface (cli.EnvSettings). Keys are ABI:
// additive only.
//
// Cluster resolution order: kubeconfig_content (written to a private 0600
// temp file) > kubeconfig_path > KUBECONFIG env > ~/.kube/config > in-cluster
// service account (when running inside a pod) — the client-go default chain.
type ConfigOptions struct {
	KubeconfigPath            string   `json:"kubeconfig_path"`
	KubeconfigContent         string   `json:"kubeconfig_content"`
	KubeContext               string   `json:"kube_context"`
	KubeToken                 string   `json:"kube_token"`
	KubeAPIServer             string   `json:"kube_apiserver"`
	KubeCaFile                string   `json:"kube_ca_file"`
	KubeTLSServerName         string   `json:"kube_tls_server_name"`
	KubeInsecureSkipTLSVerify bool     `json:"kube_insecure_skip_tls_verify"`
	KubeAsUser                string   `json:"kube_as_user"`
	KubeAsGroups              []string `json:"kube_as_groups"`
	BurstLimit                int      `json:"burst_limit"`
	QPS                       float64  `json:"qps"`
	Namespace                 string   `json:"namespace"`
	StorageDriver             string   `json:"storage_driver"` // "", "secret", "configmap", "memory", "sql"
}

// ParseConfigOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseConfigOptions(optsJSON string) (ConfigOptions, error) {
	return decodeOptions[ConfigOptions](optsJSON, "config")
}

// NewConfig builds a cluster-connected action configuration. Logging goes to
// the handler installed via helm_set_log_handler (silent by default,
// the project rules) — set the handler before creating configs. Building the
// config parses options but does not contact the cluster; the first action
// does (kubeconfig loading is lazy).
// writeTempKubeconfig persists inline kubeconfig content to an owner-only
// (0600) temp file and returns its path. The caller owns removal.
func writeTempKubeconfig(content string) (string, error) {
	f, err := os.CreateTemp("", "helm-c-kubeconfig-*")
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeIO, err)
	}
	path := f.Name()
	writeErr := os.Chmod(path, 0o600)
	if writeErr == nil {
		_, writeErr = f.WriteString(content)
	}
	if closeErr := f.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		removeBestEffort(path)
		return "", cerrors.WithCode(cerrors.CodeIO, writeErr)
	}
	return path, nil
}

// applyKubeSettings copies the connection options onto the CLI settings.
func applyKubeSettings(settings *cli.EnvSettings, opts ConfigOptions) {
	settings.KubeContext = opts.KubeContext
	settings.KubeToken = opts.KubeToken
	settings.KubeAPIServer = opts.KubeAPIServer
	settings.KubeCaFile = opts.KubeCaFile
	settings.KubeTLSServerName = opts.KubeTLSServerName
	settings.KubeInsecureSkipTLSVerify = opts.KubeInsecureSkipTLSVerify
	settings.KubeAsUser = opts.KubeAsUser
	settings.KubeAsGroups = opts.KubeAsGroups
	if opts.BurstLimit > 0 {
		settings.BurstLimit = opts.BurstLimit
	}
	if opts.QPS > 0 {
		settings.QPS = float32(opts.QPS)
	}
}

func NewConfig(opts ConfigOptions) (*Config, error) {
	if opts.KubeconfigPath != "" && opts.KubeconfigContent != "" {
		return nil, cerrors.New(cerrors.CodeInvalidArg,
			"kubeconfig_path and kubeconfig_content are mutually exclusive")
	}

	// Inline kubeconfig content is materialized once; every configuration
	// derived from this one points at the same private file.
	tempKubeconfig := ""
	if opts.KubeconfigContent != "" {
		path, err := writeTempKubeconfig(opts.KubeconfigContent)
		if err != nil {
			return nil, err
		}
		tempKubeconfig = path
		opts.KubeconfigContent = ""
		opts.KubeconfigPath = path
	}

	namespace := opts.Namespace
	if namespace == "" {
		namespace = "default"
	}

	cfg, err := newActionConfiguration(opts, namespace)
	if err != nil {
		if tempKubeconfig != "" {
			removeBestEffort(tempKubeconfig)
		}
		return nil, err
	}
	return &Config{Cfg: cfg, Namespace: namespace, opts: opts, tempKubeconfig: tempKubeconfig}, nil
}

// newActionConfiguration builds and initializes an SDK configuration for
// namespace from opts. Each call gets its own EnvSettings: the SDK's REST
// client getter keeps a pointer to the settings' namespace, so two
// configurations must never share one.
func newActionConfiguration(opts ConfigOptions, namespace string) (*action.Configuration, error) {
	settings := newSettings()
	if opts.KubeconfigPath != "" {
		settings.KubeConfig = opts.KubeconfigPath
	}
	applyKubeSettings(settings, opts)
	settings.SetNamespace(namespace)

	cfg := action.NewConfiguration(action.ConfigurationSetLogger(CurrentLogHandler()))
	if err := cfg.Init(settings.RESTClientGetter(), namespace, opts.StorageDriver); err != nil {
		return nil, cerrors.WithCode(cerrors.CodeKube, err)
	}
	return cfg, nil
}

// forNamespace returns the configuration to run an action in namespace: the
// shared one when it matches, otherwise a fresh sibling initialized for that
// namespace ("" = all namespaces, as `helm list -A` does). The shared
// configuration is never re-initialized in place.
func (c *Config) forNamespace(namespace string) (*action.Configuration, error) {
	if namespace == c.Namespace {
		return c.Cfg, nil
	}
	return newActionConfiguration(c.opts, namespace)
}

// detachedConfiguration returns a throwaway copy of the shared configuration
// for an SDK call that is known to mutate its Configuration. The copy shares
// the live clients and storage, so the call behaves identically, but whatever
// it replaces (client-side dry runs swap in fakes) dies with the copy.
func (c *Config) detachedConfiguration() *action.Configuration {
	cp := action.NewConfiguration(action.ConfigurationSetLogger(c.Cfg.Logger().Handler()))
	cp.RESTClientGetter = c.Cfg.RESTClientGetter
	cp.Releases = c.Cfg.Releases
	cp.KubeClient = c.Cfg.KubeClient
	cp.RegistryClient = c.Cfg.RegistryClient
	cp.Capabilities = c.Cfg.Capabilities
	cp.CustomTemplateFuncs = c.Cfg.CustomTemplateFuncs
	cp.HookOutputFunc = c.Cfg.HookOutputFunc
	return cp
}

// AsConfig recovers the concrete config from a registry object, keeping SDK
// types out of the capi package.
func AsConfig(obj any) (*Config, error) {
	return as[*Config](obj, "a config")
}
