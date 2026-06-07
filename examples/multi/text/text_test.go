package text_test

import (
	"fmt"

	"example.com/multi/text"
)

func ExampleNew() {
	fmt.Println(text.New(40).Width)
	// Output: 40
}

func ExampleWrap() {
	fmt.Println(text.Wrap("the quick brown fox jumps over the lazy dog", text.Options{Width: 20}))
	// Output: [the quick brown fox jumps over the lazy dog]
}

func ExampleConvert() {
	s, _ := text.Convert("hello THERE", text.Title)
	fmt.Println(s)
	// Output: Hello There
}

func ExampleConvert_unknown() {
	_, err := text.Convert("hi", "sideways")
	fmt.Println(err)
	// Output: unknown casing "sideways"
}
