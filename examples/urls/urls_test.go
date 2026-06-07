package urls_test

import (
	"fmt"

	"example.com/urls"
)

// Every call below with literal arguments is recorded by gowasm and replayed as
// a TypeScript test, so these examples document the package and pin down the
// generated npm package's behaviour at the same time.

func ExampleExtractURLs() {
	matches, _ := urls.ExtractURLs("see https://go.dev for docs", urls.Relaxed)
	fmt.Println(matches[0].Host)
	// Output: go.dev
}

func ExampleExtractURLs_strict() {
	matches, _ := urls.ExtractURLs("bare go.dev is ignored, https://go.dev is not", urls.Strict)
	fmt.Println(len(matches))
	// Output: 1
}

func ExampleExtractURLs_unknownMode() {
	_, err := urls.ExtractURLs("anything", "sloppy")
	fmt.Println(err)
	// Output: unknown strictness "sloppy", want "relaxed" or "strict"
}

func ExampleSum() {
	fmt.Println(urls.Sum([]int{1, 2, 3, 4}))
	// Output: 10
}

func ExampleHash() {
	fmt.Println(urls.Hash([]byte("hello")))
	// Output: 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
}

func ExampleParseWhen() {
	when, _ := urls.ParseWhen("2026-09-02T10:30:00Z")
	fmt.Println(when.Year())
	// Output: 2026
}

func ExampleParseWhen_invalid() {
	_, err := urls.ParseWhen("nonsense")
	fmt.Println(err != nil)
	// Output: true
}

func ExampleCounts() {
	n, hosts, _ := urls.Counts("https://go.dev https://go.dev https://npmjs.com")
	fmt.Println(n, hosts)
	// Output: 3 2
}

func ExampleTally() {
	tally, _ := urls.Tally("https://go.dev https://go.dev https://npmjs.com")
	fmt.Println(tally["go.dev"])
	// Output: 2
}

func ExampleFirstHost() {
	first, _ := urls.FirstHost("nothing to see")
	fmt.Println(first == nil)
	// Output: true
}

func ExamplePing() {
	urls.Ping()
	fmt.Println("ok")
	// Output: ok
}
