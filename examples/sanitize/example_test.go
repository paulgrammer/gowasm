package sanitize_test

import (
	"fmt"

	"example.com/sanitize"
)

func ExampleClean() {
	r, _ := sanitize.Clean(`<p>hello <script>alert(1)</script><b>world</b></p>`, sanitize.Basic)
	fmt.Println(r.HTML)
	fmt.Println(r.Removed, r.Changed)
	// Output:
	// <p>hello <b>world</b></p>
	// [script] true
}

func ExampleClean_stripsEventHandlers() {
	r, _ := sanitize.Clean(`<b onclick="steal()">click</b>`, sanitize.Basic)
	fmt.Println(r.HTML)
	// Output: <b>click</b>
}

func ExampleClean_stripsJavascriptURLs() {
	r, _ := sanitize.Clean(`<a href="javascript:alert(1)">x</a>`, sanitize.Basic)
	fmt.Println(r.HTML)
	// Output: x
}

func ExampleClean_keepsSafeLinks() {
	r, _ := sanitize.Clean(`<a href="https://go.dev">go</a>`, sanitize.Basic)
	fmt.Println(r.HTML)
	// Output: <a href="https://go.dev" rel="nofollow">go</a>
}

func ExampleClean_unknownPolicy() {
	_, err := sanitize.Clean("<p>x</p>", "lenient")
	fmt.Println(err)
	// Output: unknown policy "lenient", want "strict", "basic" or "article"
}

func ExampleText() {
	out, _ := sanitize.Text(`<h1>Title</h1><p>Body <em>text</em></p>`)
	fmt.Println(out)
	// Output: TitleBody text
}

func ExampleIsSafe() {
	ok, _ := sanitize.IsSafe("<b>fine</b>", sanitize.Basic)
	bad, _ := sanitize.IsSafe("<img src=x onerror=alert(1)>", sanitize.Basic)
	fmt.Println(ok, bad)
	// Output: true false
}
