package wrapper

import (
	"os"
	"path/filepath"

	"helm.sh/helm/v4/pkg/action"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// PackageOptions is the JSON options contract of helm_package_run. Keys are
// ABI: additive only, documented in docs/API.md.
type PackageOptions struct {
	Destination string `json:"destination"`
	Version     string `json:"version"`
	AppVersion  string `json:"app_version"`

	// Signing (`helm package --sign`): a PGP secret keyring plus the key
	// name in it; passphrase_file unlocks a protected key.
	Sign           bool   `json:"sign"`
	Key            string `json:"key"`
	Keyring        string `json:"keyring"`
	PassphraseFile string `json:"passphrase_file"`

	// DependencyUpdate fetches declared dependencies before packaging
	// (`--dependency-update`), through the same private-cache path as
	// helm_dependency_update; the repository credentials below apply to it.
	DependencyUpdate      bool   `json:"dependency_update"`
	PlainHTTP             bool   `json:"plain_http"`
	Username              string `json:"username"`
	Password              string `json:"password"`
	CertFile              string `json:"cert_file"`
	KeyFile               string `json:"key_file"`
	CaFile                string `json:"ca_file"`
	InsecureSkipTLSVerify bool   `json:"insecure_skip_tls_verify"`
}

// ParsePackageOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParsePackageOptions(optsJSON string) (PackageOptions, error) {
	return decodeOptions[PackageOptions](optsJSON, "package")
}

// PackageChart archives the chart at path into a .tgz and returns the
// archive path. With opts.Sign the archive is clear-signed and a
// "<archive>.prov" file is written alongside it.
func PackageChart(path string, opts PackageOptions) (string, error) {
	if opts.DependencyUpdate {
		if err := DependencyUpdate(path, DependencyOptions{
			PlainHTTP: opts.PlainHTTP,
			Username:  opts.Username,
			Password:  opts.Password,
			CertFile:  opts.CertFile,
			KeyFile:   opts.KeyFile,
			CaFile:    opts.CaFile,
			Insecure:  opts.InsecureSkipTLSVerify,
		}); err != nil {
			return "", err
		}
	}

	p := action.NewPackage()
	p.Destination = opts.Destination
	if p.Destination == "" {
		p.Destination = "."
	}
	p.Version = opts.Version
	p.AppVersion = opts.AppVersion

	// The SDK's own Package.Run signs only when it is driven by the CLI's
	// flags; helm-c signs explicitly after packaging so the same code path
	// serves helm_chart_sign for archives packaged elsewhere.
	out, err := p.Run(path, nil)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeChartInvalid, err)
	}
	if opts.Sign {
		if _, err := SignChart(out, SignOptions{Key: opts.Key, Keyring: opts.Keyring, PassphraseFile: opts.PassphraseFile}); err != nil {
			return "", err
		}
	}
	return out, nil
}

// SignOptions is the JSON options contract of helm_chart_sign. Keys are
// ABI: additive only.
type SignOptions struct {
	Key            string `json:"key"`     // name of the signing key in the keyring ("" = first)
	Keyring        string `json:"keyring"` // PGP secret keyring file
	PassphraseFile string `json:"passphrase_file"`
}

// ParseSignOptions strictly decodes optsJSON (docs/DESIGN.md §4).
func ParseSignOptions(optsJSON string) (SignOptions, error) {
	return decodeOptions[SignOptions](optsJSON, "sign")
}

// SignChart clear-signs a packaged chart archive with a PGP key from
// opts.Keyring and writes "<archive>.prov" next to it (SDK
// Package.Clearsign), returning that provenance path. A protected key is
// unlocked with the passphrase read from opts.PassphraseFile; without one, a
// protected key is an error rather than an interactive prompt — a library
// must never block on a terminal.
func SignChart(archivePath string, opts SignOptions) (string, error) {
	if opts.Keyring == "" {
		return "", cerrors.New(cerrors.CodeInvalidArg, "keyring is required for signing")
	}
	// Like `helm package --sign`, the key must be named: the SDK selects no
	// signing entity for an empty name and fails later with a less useful
	// "private key not found".
	if opts.Key == "" {
		return "", cerrors.New(cerrors.CodeInvalidArg, "key (the signing identity in the keyring) is required for signing")
	}
	if _, err := os.Stat(archivePath); err != nil {
		return "", cerrors.WithCode(cerrors.CodeIO, err)
	}
	p := action.NewPackage()
	p.Sign = true
	p.Key = opts.Key
	p.Keyring = opts.Keyring
	p.PassphraseFile = opts.PassphraseFile
	if p.PassphraseFile == "" {
		// The SDK falls back to prompting on the terminal for a protected
		// key; hand it an empty passphrase file instead so a locked key
		// fails fast with the SDK's own error.
		empty, err := os.CreateTemp("", "helm-c-passphrase-*")
		if err != nil {
			return "", cerrors.WithCode(cerrors.CodeIO, err)
		}
		// One empty line: the SDK reads the first line as the passphrase
		// and treats a file with no line at all as an EOF error.
		_, writeErr := empty.WriteString("\n")
		_ = empty.Close()
		defer removeBestEffort(empty.Name())
		if writeErr != nil {
			return "", cerrors.WithCode(cerrors.CodeIO, writeErr)
		}
		p.PassphraseFile = empty.Name()
	}
	if err := p.Clearsign(archivePath); err != nil {
		return "", cerrors.WithCode(cerrors.CodeChartInvalid, err)
	}
	return filepath.Clean(archivePath) + ".prov", nil
}
