// Package dataurl encodes and decodes inline data URLs without performing I/O.
package dataurl

import (
	"encoding/base64"
	"errors"
	"fmt"
	"maps"
	"mime"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// ErrInvalid reports an invalid media type, data URL, or payload.
// Errors do not contain the supplied URL or payload.
var ErrInvalid = errors.New("dataurl: invalid input")

// Encode returns a base64 data URL holding data. mediaType must be a valid
// MIME type, optionally with parameters, such as "text/plain; charset=utf-8".
// Parameter values must be valid UTF-8 without ASCII control characters and
// are percent-escaped in the URL, as are reserved bytes in types and parameter
// names. Invalid media types return an error matching [ErrInvalid] and an empty
// string. Encode does not retain data.
func Encode(mediaType string, data []byte) (string, error) {
	typ, params, err := mime.ParseMediaType(mediaType)
	if err != nil || !validMediaType(typ, params) {
		return "", fmt.Errorf("%w: media type", ErrInvalid)
	}
	var b strings.Builder
	b.WriteString("data:")
	top, sub, _ := strings.Cut(typ, "/")
	b.WriteString(url.PathEscape(top))
	b.WriteByte('/')
	b.WriteString(url.PathEscape(sub))
	for _, key := range slices.Sorted(maps.Keys(params)) {
		b.WriteByte(';')
		b.WriteString(url.PathEscape(key))
		b.WriteByte('=')
		b.WriteString(url.PathEscape(params[key]))
	}
	b.WriteString(";base64,")
	b.WriteString(base64.StdEncoding.EncodeToString(data))
	return b.String(), nil
}

// Decode returns a data URL's MIME type, including its parameters, and payload.
// The media type is formatted with [mime.FormatMediaType]; an omitted type
// defaults to text/plain with charset=US-ASCII. Types and parameter names and
// values use URL percent-escaping, rather than MIME quoted strings. Duplicate
// parameters and parameter names containing "*" (MIME continuation syntax) are
// rejected.
// Parameter values must be valid UTF-8 without ASCII control characters.
//
// Surrounding whitespace is ignored. The scheme and base64 marker are matched
// without regard to case. Base64 payloads may be percent-encoded, contain ASCII
// whitespace, omit padding or use the URL-safe alphabet. Supplied padding must
// be valid. Non-base64 payloads are percent-decoded without changing "+" to space.
//
// On failure Decode returns "", nil and an error matching [ErrInvalid]. The
// returned bytes belong to the caller. Callers must bound untrusted input size
// before decoding; Decode has no size limit and does not validate content safety.
func Decode(s string) (mediaType string, data []byte, err error) {
	s = strings.TrimSpace(s)
	if len(s) < 5 || !strings.EqualFold(s[:5], "data:") {
		return "", nil, fmt.Errorf("%w: scheme", ErrInvalid)
	}
	meta, payload, found := strings.Cut(s[5:], ",")
	if !found {
		return "", nil, fmt.Errorf("%w: missing comma", ErrInvalid)
	}
	isBase64 := false
	if before, param, ok := strings.CutLast(meta, ";"); ok && strings.EqualFold(strings.TrimSpace(param), "base64") {
		meta, isBase64 = before, true
	}
	mediaType, err = decodeMediaType(meta)
	if err != nil {
		return "", nil, err
	}
	unescaped, err := url.PathUnescape(payload)
	if err != nil {
		return "", nil, fmt.Errorf("%w: payload escape", ErrInvalid)
	}
	if isBase64 {
		var ok bool
		data, ok = decodeBase64(unescaped)
		if !ok {
			return "", nil, fmt.Errorf("%w: base64 payload", ErrInvalid)
		}
	} else {
		data = []byte(unescaped)
	}
	return mediaType, data, nil
}

func decodeMediaType(meta string) (string, error) {
	fields := strings.Split(meta, ";")
	typ, err := url.PathUnescape(strings.TrimSpace(fields[0]))
	if err != nil {
		return "", fmt.Errorf("%w: media type escape", ErrInvalid)
	}
	typ = strings.ToLower(typ)
	params := make(map[string]string)
	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(strings.TrimSpace(field), "=")
		if !ok || strings.ContainsAny(value, "\"\\") {
			return "", fmt.Errorf("%w: media parameter", ErrInvalid)
		}
		key, err := url.PathUnescape(key)
		if err != nil {
			return "", fmt.Errorf("%w: media parameter escape", ErrInvalid)
		}
		key = strings.ToLower(key)
		_, duplicate := params[key]
		value, err = url.PathUnescape(value)
		if err != nil || duplicate {
			return "", fmt.Errorf("%w: media parameter", ErrInvalid)
		}
		params[key] = value
	}
	if typ == "" {
		typ = "text/plain"
		if _, ok := params["charset"]; !ok {
			params["charset"] = "US-ASCII"
		}
	}
	if !validMediaType(typ, params) {
		return "", fmt.Errorf("%w: media type", ErrInvalid)
	}
	return mime.FormatMediaType(typ, params), nil
}

func validMediaType(typ string, params map[string]string) bool {
	if !strings.Contains(typ, "/") || mime.FormatMediaType(typ, params) == "" {
		return false
	}
	for key, value := range params {
		// Continuation names would be interpreted on the next MIME parse.
		if strings.Contains(key, "*") || !utf8.ValidString(value) {
			return false
		}
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				return false
			}
		}
	}
	return true
}

// decodeBase64 decodes standard or URL-safe base64, with or without
// padding, ignoring ASCII whitespace.
func decodeBase64(s string) ([]byte, bool) {
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == '\v' {
			return -1
		}
		return r
	}, s)
	enc := base64.RawStdEncoding
	if strings.ContainsAny(s, "-_") {
		enc = base64.RawURLEncoding
	}
	if strings.Contains(s, "=") {
		enc = enc.WithPadding(base64.StdPadding)
	}
	data, err := enc.DecodeString(s)
	return data, err == nil
}
