package dataurl_test

import (
	"errors"
	"mime"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/nostosx/kitx/dataurl"
)

func TestEncode(t *testing.T) {
	t.Parallel()
	if got, err := dataurl.Encode("image/png", []byte{1, 2}); err != nil || got != "data:image/png;base64,AQI=" {
		t.Fatalf("Encode = %q", got)
	}
}

func TestDecode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, in, mediaType, data string
		ok                        bool
	}{
		{name: "base64", in: "data:image/png;base64,AQI=", mediaType: "image/png", data: "\x01\x02", ok: true},
		{name: "unpadded", in: "DATA:image/png;base64,AQI", mediaType: "image/png", data: "\x01\x02", ok: true},
		{name: "parameters", in: "data:image/svg+xml;charset=utf-8;base64,PHN2Zy8+", mediaType: "image/svg+xml; charset=utf-8", data: "<svg/>", ok: true},
		{name: "percent", in: "data:,a%20b", mediaType: "text/plain; charset=US-ASCII", data: "a b", ok: true},
		{name: "upper case base64", in: "data:IMAGE/PNG;BASE64,AQI=", mediaType: "image/png", data: "\x01\x02", ok: true},
		{name: "whitespace", in: " data: image/png ; base64 ,AQ\nI= ", mediaType: "image/png", data: "\x01\x02", ok: true},
		{name: "ASCII whitespace", in: "data:image/png;base64,A\tQ\vI\f=\r\n", mediaType: "image/png", data: "\x01\x02", ok: true},
		{name: "url-safe", in: "data:image/png;base64,-_8=", mediaType: "image/png", data: "\xfb\xff", ok: true},
		{name: "url-safe unpadded", in: "data:image/png;base64,-_8", mediaType: "image/png", data: "\xfb\xff", ok: true},
		{name: "mixed alphabets", in: "data:image/png;base64,+_8="},
		{name: "not data", in: "https://example.com/a.png"},
		{name: "no comma", in: "data:image/png;base64"},
		{name: "bad base64", in: "data:image/png;base64,!!!"},
		{name: "excess padding", in: "data:image/png;base64,AQI===="},
		{name: "incomplete padding", in: "data:image/png;base64,AQ="},
		{name: "interior padding", in: "data:image/png;base64,A=QI"},
		{name: "bad escape", in: "data:image/png;base64,AQI%xx"},
		{name: "percent base64", in: "data:;base64,AQI%3D", mediaType: "text/plain; charset=US-ASCII", data: "\x01\x02", ok: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mediaType, data, err := dataurl.Decode(tc.in)
			if (err == nil) != tc.ok || mediaType != tc.mediaType || string(data) != tc.data {
				t.Fatalf("Decode = %q, %q, %v", mediaType, data, err)
			}
			if err != nil && (!errors.Is(err, dataurl.ErrInvalid) || data != nil) {
				t.Fatalf("failure = %x, %v", data, err)
			}
		})
	}
}

func TestMediaParametersRoundTrip(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"charset": "utf-8", "name": "你好; report, v2+100%\".txt",
		"empty": "", "slash": "a/b", "literal": "%3Bbase64",
	}
	contentType := mime.FormatMediaType("Text/Plain", want)
	encoded, err := dataurl.Encode(contentType, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	mediaType, data, err := dataurl.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	typ, params, err := mime.ParseMediaType(mediaType)
	if err != nil || typ != "text/plain" || !reflect.DeepEqual(params, want) || string(data) != "hello" {
		t.Fatalf("round trip = %q, %v, %q, %v", typ, params, data, err)
	}
}

func TestEncodeURLMetadata(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, typ, key string }{
		{"fragment in subtype", "application/x#private", "name"},
		{"percent in subtype", "application/x%private", "name"},
		{"fragment in parameter", "text/plain", "x#private"},
		{"percent in parameter", "text/plain", "x%private"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := mime.FormatMediaType(tc.typ, map[string]string{tc.key: "a#b%"})
			encoded, err := dataurl.Encode(want, []byte("payload"))
			if err != nil {
				t.Fatal(err)
			}
			u, err := url.Parse(encoded)
			if err != nil || u.Fragment != "" || u.RawQuery != "" {
				t.Fatalf("encoded metadata changes URL structure: %q, %v", encoded, err)
			}
			if _, err := url.PathUnescape(u.Opaque); err != nil {
				t.Fatalf("encoded metadata has an invalid URL escape: %q, %v", encoded, err)
			}
			mt, data, err := dataurl.Decode(u.String())
			if err != nil || mt != want || string(data) != "payload" {
				t.Fatalf("URL round trip = %q, %q, %v; want %q, payload", mt, data, err, want)
			}
		})
	}
}

func TestDecodeMediaParameters(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ input, want string }{
		{"data:;charset=utf-8,hello+world", "text/plain; charset=utf-8"},
		{"data:text/plain;name=a%3Bb%2Cc%253B;BASE64,aGVsbG8=", "text/plain; name=\"a;b,c%3B\""},
		{"data:text/plain;name=%22quoted%22,", "text/plain; name=\"\\\"quoted\\\"\""},
		{"data:text/plain;base64=value,", "text/plain; base64=value"},
		{"data:application/x%23private;x%25tag=value,", "application/x#private; x%tag=value"},
	} {
		mediaType, _, err := dataurl.Decode(tc.input)
		if err != nil || mediaType != tc.want {
			t.Errorf("Decode(%q) = %q, %v; want %q", tc.input, mediaType, err, tc.want)
		}
	}
	_, data, err := dataurl.Decode("data:,hello+world")
	if err != nil || string(data) != "hello+world" {
		t.Fatalf("plain payload = %q, %v", data, err)
	}
}

func TestInvalidMediaTypes(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", "plain", "text/", "/plain", "text/plain/extra",
		"text/plain; bad", "text/plain; charset=utf-8; charset=ascii",
		"text/plain; name=\"line\r\nbreak\"", "text/plain; name=\"\xff\""} {
		if got, err := dataurl.Encode(input, []byte("private")); got != "" || !errors.Is(err, dataurl.ErrInvalid) {
			t.Errorf("Encode(%q) = %q, %v", input, got, err)
		}
	}
	for _, input := range []string{
		"data:plain,private", "data:text/plain/extra,private",
		"data:text/plain;name=a;NAME=a,private", "data:text/plain;name*=utf-8''x,private",
		"data:text/plain;name=%0D%0AX-secret:abc,private", "data:text/plain;name=%FF,private",
		"data:text/plain;name=%ZZ,private", "data:text/plain;name=\"raw\",private",
		"data:text/plain;base64;name=x,private",
		"data:text/plain;name=a;%6Eame=b,private", "data:text/plain;name%ZZ=a,private",
		"data:text/plain;name%2A=a,private", "data:application/x%ZZ,private",
	} {
		mt, data, err := dataurl.Decode(input)
		if mt != "" || data != nil || !errors.Is(err, dataurl.ErrInvalid) {
			t.Errorf("Decode(%q) = %q, %x, %v", input, mt, data, err)
		}
		if err != nil && (strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "X-secret")) {
			t.Errorf("error discloses input: %v", err)
		}
	}
}
