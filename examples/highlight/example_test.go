package highlight_test

import (
	"fmt"

	"example.com/highlight"
)

func ExampleTokenize() {
	r, _ := highlight.Tokenize("go", "monokai", "func main() {}")
	fmt.Println(r.Language, r.Style, r.Tokens[0].Kind, r.Tokens[0].Text)
	// Output: Go monokai KeywordDeclaration func
}

func ExampleTokenize_unknownLanguage() {
	_, err := highlight.Tokenize("cobol-2077", "monokai", "x")
	fmt.Println(err)
	// Output: no lexer for "cobol-2077"; call languages to see what is available
}

func ExampleTokenize_unknownStyle() {
	_, err := highlight.Tokenize("go", "midnight-neon", "x")
	fmt.Println(err)
	// Output: no style named "midnight-neon"; call styles to see what is available
}

func ExampleDetect() {
	d, _ := highlight.Detect("server.rs", "")
	fmt.Println(d.Language.Name, d.By)
	// Output: Rust filename
}

func ExampleDetect_contentIsOnlyAGuess() {
	// With no filename there is only the content, and the heuristic is weak:
	// this Go source is identified as GDScript. Pass a filename when you have
	// one, which is why the result says how it was decided.
	d, _ := highlight.Detect("", "package main\nfunc main(){}")
	fmt.Println(d.Language.Name, d.By)
	// Output: GDScript3 content
}

func ExampleDetect_unknown() {
	_, err := highlight.Detect("", "zzzz")
	fmt.Println(err)
	// Output: could not identify the language
}

func ExampleKinds() {
	kinds, _ := highlight.Kinds("json", `{"a": 1}`)
	fmt.Println(len(kinds) > 0)
	// Output: true
}
