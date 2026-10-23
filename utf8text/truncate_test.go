package utf8text_test

import (
	"math"
	"testing"
	"unicode/utf8"

	"github.com/nostosx/kitx/utf8text"
)

func TestTruncate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input, suffix string
		limit               int
		want                string
	}{
		{name: "fits", input: "hello", limit: 5, suffix: "...", want: "hello"},
		{name: "ASCII", input: "hello world", limit: 8, suffix: "...", want: "hello..."},
		{name: "rune boundary", input: "你好世界", limit: 7, suffix: "...", want: "你..."},
		{name: "emoji boundary", input: "ab🙂c", limit: 6, suffix: "…", want: "ab…"},
		{name: "suffix fills budget", input: "hello", limit: 2, suffix: "...", want: ".."},
		{name: "suffix rune does not fit", input: "hello", limit: 2, suffix: "…", want: "he"},
		{name: "empty suffix", input: "你好世界", limit: 7, want: "你好"},
		{name: "zero", input: "hello", suffix: "..."},
		{name: "negative", input: "hello", limit: -1, suffix: "..."},
		{name: "empty", limit: 3, suffix: "..."},
		{name: "invalid within budget", input: "bad\xfftext", limit: 20, want: "badtext"},
		{name: "invalid before truncation", input: "ab\xff你好", limit: 5, suffix: ".", want: "ab."},
		{name: "invalid suffix", input: "hello world", limit: 8, suffix: "\xff...", want: "hello..."},
		{name: "max int", input: "hello", limit: math.MaxInt, suffix: "...", want: "hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := utf8text.Truncate(tc.input, tc.limit, tc.suffix)
			if got != tc.want || !utf8.ValidString(got) || len(got) > max(tc.limit, 0) {
				t.Errorf("Truncate = %q, want %q (budget %d)", got, tc.want, tc.limit)
			}
		})
	}
}
