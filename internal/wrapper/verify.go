package wrapper

import (
	"helm.sh/helm/v4/pkg/downloader"

	"github.com/shivamkumar99/helm-c-sdk/internal/cerrors"
)

// VerifyChart checks a chart archive against its provenance signature.
// provFile may be "" (defaults to <path>.prov); keyring is a GPG public
// keyring file. Returns {"file_name","file_hash","signed_by":[...]} JSON on
// success; a failed signature is HELM_ERR_CHART_INVALID.
func VerifyChart(path, provFile, keyring string) (string, error) {
	if provFile == "" {
		provFile = path + ".prov"
	}
	v, err := downloader.VerifyChart(path, provFile, keyring)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeChartInvalid, err)
	}

	out := map[string]any{
		"file_name": v.FileName,
		"file_hash": v.FileHash,
	}
	signers := make([]string, 0)
	if v.SignedBy != nil {
		for name := range v.SignedBy.Identities {
			signers = append(signers, name)
		}
	}
	out["signed_by"] = signers
	return marshalJSON(out)
}
