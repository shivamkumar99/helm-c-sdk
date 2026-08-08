package wrapper

import (
	"log/slog"

	"helm.sh/helm/v4/pkg/registry"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// RegistryClientOptions is the JSON options contract of
// helm_registry_client_new. Keys are ABI: additive only.
type RegistryClientOptions struct {
	Debug           bool   `json:"debug"`
	PlainHTTP       bool   `json:"plain_http"`
	CredentialsFile string `json:"credentials_file"`
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
	c, err := registry.NewClient(copts...)
	if err != nil {
		return nil, cerrors.WithCode(cerrors.CodeRegistry, err)
	}
	return c, nil
}

// AsRegistryClient recovers the concrete client from a registry object,
// keeping SDK types out of the capi package.
func AsRegistryClient(obj any) (*registry.Client, error) {
	c, ok := obj.(*registry.Client)
	if !ok {
		return nil, cerrors.New(cerrors.CodeWrongHandleType, "handle does not hold a registry client")
	}
	return c, nil
}

// LoginOptions is the JSON options contract of helm_registry_login. Keys are
// ABI: additive only.
type LoginOptions struct {
	Insecure  bool `json:"insecure"`
	PlainHTTP bool `json:"plain_http"`
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
	return cerrors.WithCode(cerrors.CodeRegistry, c.Login(host,
		registry.LoginOptBasicAuth(username, password),
		registry.LoginOptInsecure(opts.Insecure),
		registry.LoginOptPlainText(opts.PlainHTTP),
	))
}

// RegistryLogout removes the client's stored credentials for host.
func RegistryLogout(clientObj any, host string) error {
	c, err := AsRegistryClient(clientObj)
	if err != nil {
		return err
	}
	return cerrors.WithCode(cerrors.CodeRegistry, c.Logout(host))
}
