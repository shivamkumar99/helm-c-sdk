package wrapper

import (
	"bytes"
	"testing"

	"helm.sh/helm/v4/pkg/chart/v2/loader"
)

// FuzzParseSetString hammers the --set parser with arbitrary input; any panic
// is a finding (parse errors are expected and fine).
func FuzzParseSetString(f *testing.F) {
	f.Add("a=1,b=two")
	f.Add("ports={80,443}")
	f.Add("image.tag=v2")
	f.Add("a[0].b=1,a[1].c=x")
	f.Add("=,=,{}")
	f.Fuzz(func(_ *testing.T, s string) {
		_, _ = ParseSetString(s)
	})
}

// FuzzLoadArchive feeds arbitrary bytes to the chart archive loader we build
// on; any panic or unbounded resource use is a finding. The SDK's
// MaxDecompressed* limits are the DoS guard under test.
func FuzzLoadArchive(f *testing.F) {
	f.Add([]byte("not a tgz"))
	f.Add([]byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00})
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = loader.LoadArchive(bytes.NewReader(data))
	})
}

// FuzzValuesRoundTrip: arbitrary values JSON through unmarshal must never
// panic, and valid JSON must survive a marshal round trip.
func FuzzValuesRoundTrip(f *testing.F) {
	f.Add(`{"a":1}`)
	f.Add(`{"nested":{"deep":[1,2,{"x":null}]}}`)
	f.Add(`not json`)
	f.Fuzz(func(t *testing.T, s string) {
		vals, err := unmarshalValues(s)
		if err != nil || vals == nil {
			return
		}
		if _, err := marshalJSON(vals); err != nil {
			t.Fatalf("valid values failed to re-marshal: %v", err)
		}
	})
}
