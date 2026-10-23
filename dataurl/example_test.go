package dataurl_test

import (
	"fmt"

	"github.com/nostosx/kitx/dataurl"
)

func ExampleEncode() {
	encoded, err := dataurl.Encode("text/plain; charset=utf-8", []byte("hello"))
	if err != nil {
		panic(err)
	}
	fmt.Println(encoded)
	// Output: data:text/plain;charset=utf-8;base64,aGVsbG8=
}

func ExampleDecode() {
	mediaType, data, err := dataurl.Decode("data:;charset=utf-8,hello%20world")
	if err != nil {
		panic(err)
	}
	fmt.Printf("%s: %s\n", mediaType, data)
	// Output: text/plain; charset=utf-8: hello world
}
