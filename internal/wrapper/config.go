package wrapper

import (
	"os"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/cli"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

// Config bundles an initialized action.Configuration with its target
// namespace. One Config supports parallel actions on different releases;
// concurrent writes to the same release can corrupt its history (SDK
// semantics) — bindings must serialize per release.
type Config struct {
	Cfg       *action.Configuration
	Namespace string

	// tempKubeconfig holds the private file backing kubeconfig_content; it
	// must outlive the config (loading is lazy) and is removed by Close.
	tempKubeconfig string
}

// Close releases resources owned by the config. Idempotent.
func (c *Config) Close() {
	if c.tempKubeconfig != "" {
		os.Remove(c.tempKubeconfig)
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

// ParseConfigOptions strictly decodes optsJSON (ADR-0004).
func ParseConfigOptions(optsJSON string) (ConfigOptions, error) {
	return decodeOptions[ConfigOptions](optsJSON, "config")
}

// NewConfig builds a cluster-connected action configuration. Logging goes to
// the handler installed via helm_set_log_handler (silent by default,
// the project rules) — set the handler before creating configs. Building the
// config parses options but does not contact the cluster; the first action
// does (kubeconfig loading is lazy).
func NewConfig(opts ConfigOptions) (*Config, error) {
	if opts.KubeconfigPath != "" && opts.KubeconfigContent != "" {
		return nil, cerrors.New(cerrors.CodeInvalidArg,
			"kubeconfig_path and kubeconfig_content are mutually exclusive")
	}

	settings := cli.New()
	tempKubeconfig := ""
	if opts.KubeconfigContent != "" {
		f, err := os.CreateTemp("", "helm-c-kubeconfig-*")
		if err != nil {
			return nil, cerrors.WithCode(cerrors.CodeIO, err)
		}
		tempKubeconfig = f.Name()
		if err := os.Chmod(tempKubeconfig, 0o600); err == nil {
			_, err = f.WriteString(opts.KubeconfigContent)
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(tempKubeconfig)
			return nil, cerrors.WithCode(cerrors.CodeIO, err)
		}
		settings.KubeConfig = tempKubeconfig
	} else if opts.KubeconfigPath != "" {
		settings.KubeConfig = opts.KubeconfigPath
	}

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

	namespace := opts.Namespace
	if namespace == "" {
		namespace = "default"
	}
	settings.SetNamespace(namespace)

	cfg := action.NewConfiguration(action.ConfigurationSetLogger(CurrentLogHandler()))
	if err := cfg.Init(settings.RESTClientGetter(), namespace, opts.StorageDriver); err != nil {
		if tempKubeconfig != "" {
			os.Remove(tempKubeconfig)
		}
		return nil, cerrors.WithCode(cerrors.CodeKube, err)
	}
	return &Config{Cfg: cfg, Namespace: namespace, tempKubeconfig: tempKubeconfig}, nil
}

// AsConfig recovers the concrete config from a registry object, keeping SDK
// types out of the capi package.
func AsConfig(obj any) (*Config, error) {
	c, ok := obj.(*Config)
	if !ok {
		return nil, cerrors.New(cerrors.CodeWrongHandleType, "handle does not hold a config")
	}
	return c, nil
}
