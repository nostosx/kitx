package dataurl_test

import (
	"bytes"
	"errors"
	"mime"
	"net/url"
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/nostosx/kitx/dataurl"
)

func FuzzRoundTrip(f *testing.F) {
	f.Add([]byte{0, 1, 255}, "a; b,+%20你好")
	f.Add([]byte{}, "")
	f.Fuzz(func(t *testing.T, input []byte, name string) {
		if !utf8.ValidString(name) {
			return
		}
		for _, r := range name {
			if r < 0x20 || r == 0x7f {
				return
			}
		}
		want := map[string]string{"name": name, "charset": "utf-8"}
		encoded, err := dataurl.Encode(mime.FormatMediaType("text/plain", want), input)
		if err != nil {
			t.Fatal(err)
		}
		u, err := url.Parse(encoded)
		if err != nil || u.Fragment != "" || u.RawQuery != "" {
			t.Fatalf("invalid URL structure: %q, %v", encoded, err)
		}
		if _, err := url.PathUnescape(u.Opaque); err != nil {
			t.Fatalf("invalid URL escape: %q, %v", encoded, err)
		}
		mediaType, got, err := dataurl.Decode(encoded)
		if err != nil {
			t.Fatal(err)
		}
		typ, params, err := mime.ParseMediaType(mediaType)
		if err != nil || typ != "text/plain" || !reflect.DeepEqual(params, want) || !bytes.Equal(got, input) {
			t.Fatalf("round trip = %q, %v, %x, %v", typ, params, got, err)
		}
	})
}

func FuzzDecode(f *testing.F) {
	f.Add("data:text/plain;name=a%3Bb;base64,YQ==")
	f.Add("data:,hello+world")
	f.Add("data:text/plain;name=%FF,private")
	f.Add("data:application/x%23private;x%25tag=value,hello")
	f.Add("data:text/plain;name=a;%6Eame=b,private")
	f.Fuzz(func(t *testing.T, input string) {
		mt, data, err := dataurl.Decode(input)
		if err != nil {
			if mt != "" || data != nil || !errors.Is(err, dataurl.ErrInvalid) {
				t.Fatalf("partial result: %q %x %v", mt, data, err)
			}
			return
		}
		encoded, err := dataurl.Encode(mt, data)
		if err != nil {
			t.Fatal(err)
		}
		mt2, data2, err := dataurl.Decode(encoded)
		if err != nil || mt2 != mt || !bytes.Equal(data, data2) {
			t.Fatalf("unstable result: %q %q %v", mt, mt2, err)
		}
	})
}
