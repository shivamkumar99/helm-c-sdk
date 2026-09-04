package wrapper

import (
	"log/slog"
	"strings"

	"helm.sh/helm/v4/pkg/registry"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// RegistryClientOptions is the JSON options contract of
// helm_registry_client_new. Keys are ABI: additive only.
type RegistryClientOptions struct {
	Debug           bool   `json:"debug"`
	PlainHTTP       bool   `json:"plain_http"`
	CredentialsFile string `json:"credentials_file"`
	// Username/Password set static basic-auth credentials on the client
	// itself (SDK ClientOptBasicAuth) — no login call, nothing persisted.
	Username string `json:"username"`
	Password string `json:"password"`
}

// ParseRegistryClientOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseRegistryClientOptions(optsJSON string) (RegistryClientOptions, error) {
	return decodeOptions[RegistryClientOptions](optsJSON, "registry client")
}

// NewRegistryClient builds an OCI registry client. Client output rides the
// installed log handler at debug level (silent while no handler is set);
// credentials land in CredentialsFile (or helm's default registry config
// when empty).
func NewRegistryClient(opts RegistryClientOptions) (*registry.Client, error) {
	copts := []registry.ClientOption{registry.ClientOptWriter(LogWriter(slog.LevelDebug))}
	if opts.Debug {
		copts = append(copts, registry.ClientOptDebug(true))
	}
	if opts.PlainHTTP {
		copts = append(copts, registry.ClientOptPlainHTTP())
	}
	if opts.CredentialsFile != "" {
		copts = append(copts, registry.ClientOptCredentialsFile(opts.CredentialsFile))
	}
	if opts.Username != "" || opts.Password != "" {
		copts = append(copts, registry.ClientOptBasicAuth(opts.Username, opts.Password))
	}
	c, err := registry.NewClient(copts...)
	if err != nil {
		return nil, cerrors.WithCode(cerrors.CodeRegistry, err)
	}
	return c, nil
}

// AsRegistryClient recovers the concrete client from a registry object,
// keeping SDK types out of the capi package.
func AsRegistryClient(obj any) (*registry.Client, error) {
	return as[*registry.Client](obj, "a registry client")
}

// LoginOptions is the JSON options contract of helm_registry_login. Keys are
// ABI: additive only.
type LoginOptions struct {
	Insecure  bool   `json:"insecure"`
	PlainHTTP bool   `json:"plain_http"`
	CertFile  string `json:"cert_file"` // client certificate (mTLS)
	KeyFile   string `json:"key_file"`
	CaFile    string `json:"ca_file"`
}

// ParseLoginOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseLoginOptions(optsJSON string) (LoginOptions, error) {
	return decodeOptions[LoginOptions](optsJSON, "login")
}

// RegistryLogin authenticates the client against host with basic credentials.
// The password never appears in errors or logs.
func RegistryLogin(clientObj any, host, username, password string, opts LoginOptions) error {
	c, err := AsRegistryClient(clientObj)
	if err != nil {
		return err
	}
	lopts := []registry.LoginOption{
		registry.LoginOptBasicAuth(username, password),
		registry.LoginOptInsecure(opts.Insecure),
		registry.LoginOptPlainText(opts.PlainHTTP),
	}
	if opts.CertFile != "" || opts.KeyFile != "" || opts.CaFile != "" {
		lopts = append(lopts, registry.LoginOptTLSClientConfig(opts.CertFile, opts.KeyFile, opts.CaFile))
	}
	return cerrors.WithCode(cerrors.CodeRegistry, c.Login(host, lopts...))
}

// RegistryLogout removes the client's stored credentials for host.
func RegistryLogout(clientObj any, host string) error {
	c, err := AsRegistryClient(clientObj)
	if err != nil {
		return err
	}
	return cerrors.WithCode(cerrors.CodeRegistry, c.Logout(host))
}

// bareRef strips the oci:// scheme: the SDK's low-level Tags/Resolve take
// "host/repo[:tag]" while everything else in helm-c accepts full oci:// URLs.
func bareRef(ref string) string {
	return strings.TrimPrefix(ref, "oci://")
}

// RegistryTags lists the semver-compliant tags of an oci:// chart
// reference, newest first (SDK Client.Tags) — "which versions exist?" for
// OCI, the counterpart of reading an HTTP repo index. Returns a JSON array.
func RegistryTags(clientObj any, ref string) (string, error) {
	c, err := AsRegistryClient(clientObj)
	if err != nil {
		return "", err
	}
	tags, err := c.Tags(bareRef(ref))
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRegistry, err)
	}
	if tags == nil {
		tags = []string{}
	}
	return marshalJSON(tags)
}

// RegistryResolve resolves an oci:// reference to its manifest descriptor
// (SDK Client.Resolve): {"digest","media_type","size"} as JSON.
func RegistryResolve(clientObj any, ref string) (string, error) {
	c, err := AsRegistryClient(clientObj)
	if err != nil {
		return "", err
	}
	desc, err := c.Resolve(bareRef(ref))
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeRegistry, err)
	}
	return marshalJSON(map[string]any{
		"digest":     desc.Digest.String(),
		"media_type": desc.MediaType,
		"size":       desc.Size,
	})
}
