package wrapper

import (
	"os"

	"helm.sh/helm/v4/pkg/strvals"

	"github.com/shivamkumar99/helm-c-sdk/pkg/cerrors"
)

// ParseSetString parses a Helm --set expression ("a=1,b.c=two") into a JSON
// object string. Values are typed the way --set types them: "1" becomes a
// number, "true" a boolean.
func ParseSetString(s string) (string, error) {
	vals, err := strvals.Parse(s)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}
	return marshalJSON(vals)
}

// ParseSetStringValues is --set-string: every value stays a string
// ("port=80" yields "80", not 80).
func ParseSetStringValues(s string) (string, error) {
	vals, err := strvals.ParseString(s)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}
	return marshalJSON(vals)
}

// ParseSetJSON is --set-json: each value is a JSON document
// (`a={"b":[1,2]}`, `c=null`).
func ParseSetJSON(s string) (string, error) {
	vals := map[string]any{}
	if err := strvals.ParseJSON(s, vals); err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}
	return marshalJSON(vals)
}

// ParseSetLiteral is --set-literal: the value is taken verbatim, with no
// list/map/escape interpretation (`a=b,c=d` is one key with value "b,c=d").
func ParseSetLiteral(s string) (string, error) {
	vals, err := strvals.ParseLiteral(s)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}
	return marshalJSON(vals)
}

// ParseSetFile is --set-file: each value names a file whose contents become
// the value ("cert=./tls.crt").
func ParseSetFile(s string) (string, error) {
	reader := func(rs []rune) (any, error) {
		data, err := os.ReadFile(string(rs)) // #nosec G304 -- the path is the caller's own --set-file argument
		if err != nil {
			return nil, err
		}
		return string(data), nil
	}
	vals, err := strvals.ParseFile(s, reader)
	if err != nil {
		return "", cerrors.WithCode(cerrors.CodeValues, err)
	}
	return marshalJSON(vals)
}
