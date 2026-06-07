package regex_test

import (
	"fmt"
	"strings"

	"example.com/regex"
)

func ExampleFindAll() {
	matches, _ := regex.FindAll(`(?P<year>\d{4})-(?P<month>\d{2})`, "shipped 2026-09 and 2027-01")
	fmt.Println(len(matches), matches[0].Text, matches[0].Groups[0].Name)
	// Output: 2 2026-09 year
}

func ExampleTest() {
	ok, _ := regex.Test(`^\w+@\w+\.\w+$`, "ada@example.com")
	fmt.Println(ok)
	// Output: true
}

func ExampleDescribe() {
	p, _ := regex.Describe(`https://(?P<host>[^/]+)/(?P<path>.*)`)
	fmt.Println(p.Groups, p.Names, p.Literal)
	// Output: 2 [host path] https://
}

func ExampleDescribe_unsupported() {
	// Lookahead is exactly what makes an engine backtrack, so RE2 has none.
	_, err := regex.Describe(`(?=foo)bar`)
	fmt.Println(err != nil)
	// Output: true
}

func ExampleReplace() {
	out, _ := regex.Replace(`(\w+)@(\w+)`, "ada@example", "$2.$1")
	fmt.Println(out)
	// Output: example.ada
}

func ExampleSplit() {
	parts, _ := regex.Split(`\s*,\s*`, "a ,  b,c", -1)
	fmt.Println(parts)
	// Output: [a b c]
}

func ExampleTimeMatch() {
	// The classic ReDoS input. A backtracking engine takes exponential time on
	// this; RE2 returns immediately.
	t, _ := regex.TimeMatch(`(a+)+$`, strings.Repeat("a", 40)+"b")
	fmt.Println(t.Matched, t.InputLength, t.Microseconds < 100000)
	// Output: false 41 true
}
