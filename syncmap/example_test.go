package syncmap_test

import (
	"fmt"

	"github.com/nostosx/kitx/syncmap"
)

func ExampleMap() {
	var names syncmap.Map[int, string]
	names.Set(1, "Ada")
	name, ok := names.Get(1)
	fmt.Println(name, ok, names.Delete(1))
	// Output: Ada true true
}
