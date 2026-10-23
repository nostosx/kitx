package utf8text_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nostosx/kitx/utf8text"
)

func FuzzTruncate(f *testing.F) {
	f.Add("你好世界", 7, "...")
	f.Add("bad\xfftext", -1, "…")
	f.Add("", 0, "")
	f.Add("hello world", 2, "…")
	f.Fuzz(func(t *testing.T, input string, limit int, suffix string) {
		got := utf8text.Truncate(input, limit, suffix)
		if len(got) > max(limit, 0) || !utf8.ValidString(got) {
			t.Fatalf("invalid output %q for budget %d", got, limit)
		}
		clean := strings.ToValidUTF8(input, "")
		if len(clean) <= max(limit, 0) && got != clean {
			t.Fatal("text within budget changed")
		}
		if utf8text.Truncate(got, limit, suffix) != got {
			t.Fatal("truncation is not idempotent")
		}
	})
}
