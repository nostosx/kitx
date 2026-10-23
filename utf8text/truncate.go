// Package utf8text provides byte-limited operations on UTF-8 text.
package utf8text

import (
	"strings"
	"unicode/utf8"
)

// Truncate returns a valid UTF-8 prefix of s within maxBytes, including suffix
// when truncation is needed. Invalid UTF-8 in s and suffix is removed first.
// Nonpositive limits return "". If suffix alone exceeds the budget, its longest
// complete UTF-8 prefix that fits is used. Any remaining budget holds a prefix
// of s; a rune is never split to fill the budget. Inputs that fit are returned
// without a suffix.
func Truncate(s string, maxBytes int, suffix string) string {
	if maxBytes <= 0 {
		return ""
	}
	s = strings.ToValidUTF8(s, "")
	if len(s) <= maxBytes {
		return s
	}
	suffix = prefix(strings.ToValidUTF8(suffix, ""), maxBytes)
	return prefix(s, maxBytes-len(suffix)) + suffix
}

// prefix requires valid UTF-8 and a nonnegative byte limit.
func prefix(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
