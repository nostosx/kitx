package utf8text_test

import (
	"fmt"

	"github.com/nostosx/kitx/utf8text"
)

func ExampleTruncate() {
	fmt.Println(utf8text.Truncate("你好世界", 7, "..."))
	// Output: 你...
}
