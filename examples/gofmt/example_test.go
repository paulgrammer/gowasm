package gofmt_test

import (
	"fmt"

	"example.com/gofmt"
)

func ExampleFormat() {
	out, _ := gofmt.Format("package main\nfunc  main( ){x:=1;_=x}")
	fmt.Print(out)
	// Output:
	// package main
	//
	// func main() { x := 1; _ = x }
}

func ExampleFormat_invalid() {
	_, err := gofmt.Format("package main\nfunc {")
	fmt.Println(err != nil)
	// Output: true
}

func ExampleCheck() {
	errs, _ := gofmt.Check("package main\n\nfunc main() {}\n")
	fmt.Println(len(errs))
	// Output: 0
}

func ExampleCheck_reportsPositions() {
	errs, _ := gofmt.Check("package main\n\nfunc main( {\n}\n")
	fmt.Println(errs[0].Position.Line > 0, len(errs) > 0)
	// Output: true true
}

func ExampleOutline() {
	src := "package demo\n\nimport \"fmt\"\n\n// Greet says hello.\nfunc Greet(name string) string { return fmt.Sprint(name) }\n\ntype User struct{}\n"
	f, _ := gofmt.Outline(src)
	fmt.Println(f.Package, f.Imports, len(f.Decls))
	fmt.Println(f.Decls[0].Signature)
	fmt.Println(f.Decls[0].Doc)
	// Output:
	// demo [fmt] 2
	// func Greet(name string) string
	// Greet says hello.
}

func ExampleImports() {
	src := "package x\n\nimport (\n\t\"os\"\n\t\"fmt\"\n)\n"
	list, _ := gofmt.Imports(src)
	fmt.Println(list)
	// Output: [fmt os]
}

func ExampleTokenize() {
	toks, _ := gofmt.Tokenize("package main")
	fmt.Println(len(toks), toks[0].Kind, toks[0].Keyword)
	// Output: 3 package true
}
