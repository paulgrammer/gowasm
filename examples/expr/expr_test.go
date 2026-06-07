package expr_test

import (
	"fmt"

	"example.com/expr"
)

func ExampleEval() {
	v, _ := expr.Eval("price * quantity", map[string]any{"price": 4.5, "quantity": 3})
	fmt.Println(v.Kind, v.JSON)
	// Output: number 13.5
}

func ExampleTest() {
	ok, _ := expr.Test(`age >= 18 and country in ["KE", "UG", "TZ"]`,
		map[string]any{"age": 30, "country": "KE"})
	fmt.Println(ok)
	// Output: true
}

func ExampleBenchmark_invalidRuns() {
	_, err := expr.Benchmark("1 + 1", map[string]any{}, 0)
	fmt.Println(err)
	// Output: runs must be between 1 and 1000000, got 0
}

func ExampleTest_notABool() {
	_, err := expr.Test("1 + 1", map[string]any{})
	fmt.Println(err)
	// Output: "1 + 1" produced a number, but a rule must produce a bool
}

func ExampleEval_unknownVariable() {
	_, err := expr.Eval("missing + 1", map[string]any{"present": 1})
	fmt.Println(err != nil)
	// Output: true
}

func ExampleEval_noEscapeHatch() {
	// There is no require, no import, no fetch. The language simply has none,
	// so an expression cannot reach anything the host did not pass in.
	_, err := expr.Eval(`require("fs")`, map[string]any{})
	fmt.Println(err != nil)
	// Output: true
}

func ExampleCompile() {
	p, _ := expr.Compile("a + b", map[string]any{"a": 1, "b": 2})
	fmt.Println(p.Source, len(p.Disassembly) > 0)
	// Output: a + b true
}

func ExampleCompile_typeError() {
	// Caught at compile time, before any record is evaluated.
	_, err := expr.Compile(`name + 1`, map[string]any{"name": "ada"})
	fmt.Println(err != nil)
	// Output: true
}
