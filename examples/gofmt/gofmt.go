// Package gofmt exposes Go's own parser and formatter to JavaScript.
//
// This is the self-referential one: Go's go/parser, go/format and go/types,
// compiled to WebAssembly, analysing Go source inside a browser. The Go
// Playground needs a server round trip to do this. Here it happens locally,
// offline, with no request leaving the page.
//
// Everything below is standard library. No third-party parser approximates Go
// well enough to be worth using when the real one is this portable.
package gofmt

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"sort"
	"strings"
)

// Position is a location in the source.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset"`
}

// SyntaxError is one parse failure, with its position.
type SyntaxError struct {
	Message  string   `json:"message"`
	Position Position `json:"position"`
}

// Decl is one top-level declaration.
type Decl struct {
	// Kind is func, type, var, const or import.
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Exported bool     `json:"exported"`
	Doc      string   `json:"doc,omitempty"`
	Position Position `json:"position"`
	// Signature is filled in for functions.
	Signature string `json:"signature,omitempty"`
	// Receiver is set for methods.
	Receiver string `json:"receiver,omitempty"`
}

// File is what a parsed source file contains.
type File struct {
	Package string   `json:"package"`
	Imports []string `json:"imports"`
	Decls   []Decl   `json:"decls"`
	// Lines and Bytes describe the input that produced this.
	Lines int `json:"lines"`
	Bytes int `json:"bytes"`
}

// Token is one lexical token, for syntax highlighting.
type Token struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	Offset   int    `json:"offset"`
	Line     int    `json:"line"`
	Keyword  bool   `json:"keyword,omitempty"`
	Literal  bool   `json:"literal,omitempty"`
	Operator bool   `json:"operator,omitempty"`
}

func positionOf(fset *token.FileSet, p token.Pos) Position {
	pos := fset.Position(p)
	return Position{Line: pos.Line, Column: pos.Column, Offset: pos.Offset}
}

// syntaxErrors converts a parse error into structured positions rather than a
// single opaque string, which is what an editor needs to place a squiggle.
func syntaxErrors(fset *token.FileSet, err error) []SyntaxError {
	var list scanner.ErrorList
	if !asErrorList(err, &list) {
		return []SyntaxError{{Message: err.Error()}}
	}
	out := make([]SyntaxError, 0, len(list))
	for _, e := range list {
		out = append(out, SyntaxError{
			Message:  e.Msg,
			Position: Position{Line: e.Pos.Line, Column: e.Pos.Column, Offset: e.Pos.Offset},
		})
	}
	return out
}

func asErrorList(err error, out *scanner.ErrorList) bool {
	if list, ok := err.(scanner.ErrorList); ok {
		*out = list
		return true
	}
	return false
}

// Format runs gofmt over the source, returning it canonically formatted.
func Format(source string) (string, error) {
	out, err := format.Source([]byte(source))
	if err != nil {
		return "", fmt.Errorf("cannot format: %w", err)
	}
	return string(out), nil
}

// Check parses the source and reports every syntax error with its position.
// An empty result means the source parses.
func Check(source string) ([]SyntaxError, error) {
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "input.go", source, parser.AllErrors)
	if err == nil {
		return []SyntaxError{}, nil
	}
	return syntaxErrors(fset, err), nil
}

// Outline parses the source and describes what it declares.
func Outline(source string) (File, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "input.go", source, parser.ParseComments)
	if err != nil {
		return File{}, fmt.Errorf("cannot parse: %w", err)
	}

	out := File{
		Package: f.Name.Name,
		Imports: []string{},
		Decls:   []Decl{},
		Bytes:   len(source),
		Lines:   strings.Count(source, "\n") + 1,
	}
	for _, imp := range f.Imports {
		out.Imports = append(out.Imports, strings.Trim(imp.Path.Value, `"`))
	}

	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			entry := Decl{
				Kind:      "func",
				Name:      d.Name.Name,
				Exported:  d.Name.IsExported(),
				Position:  positionOf(fset, d.Name.Pos()),
				Signature: renderSignature(fset, d),
			}
			if d.Doc != nil {
				entry.Doc = strings.TrimSpace(d.Doc.Text())
			}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				entry.Receiver = renderNode(fset, d.Recv.List[0].Type)
			}
			out.Decls = append(out.Decls, entry)

		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					entry := Decl{
						Kind:     "type",
						Name:     s.Name.Name,
						Exported: s.Name.IsExported(),
						Position: positionOf(fset, s.Name.Pos()),
					}
					if doc := s.Doc; doc != nil {
						entry.Doc = strings.TrimSpace(doc.Text())
					} else if d.Doc != nil {
						entry.Doc = strings.TrimSpace(d.Doc.Text())
					}
					out.Decls = append(out.Decls, entry)
				case *ast.ValueSpec:
					kind := "var"
					if d.Tok == token.CONST {
						kind = "const"
					}
					for _, name := range s.Names {
						out.Decls = append(out.Decls, Decl{
							Kind:     kind,
							Name:     name.Name,
							Exported: name.IsExported(),
							Position: positionOf(fset, name.Pos()),
						})
					}
				}
			}
		}
	}
	return out, nil
}

func renderNode(fset *token.FileSet, n ast.Node) string {
	var b strings.Builder
	if err := format.Node(&b, fset, n); err != nil {
		return ""
	}
	return b.String()
}

func renderSignature(fset *token.FileSet, d *ast.FuncDecl) string {
	// The body is dropped so the signature reads on one line.
	clone := *d
	clone.Body = nil
	clone.Doc = nil
	return strings.TrimSuffix(renderNode(fset, &clone), "\n")
}

// Tokenize splits the source into lexical tokens, which is enough to drive a
// syntax highlighter without shipping a Go grammar to the browser.
func Tokenize(source string) ([]Token, error) {
	fset := token.NewFileSet()
	file := fset.AddFile("input.go", fset.Base(), len(source))

	var s scanner.Scanner
	var errs int
	s.Init(file, []byte(source), func(token.Position, string) { errs++ }, scanner.ScanComments)

	out := []Token{}
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		text := lit
		if text == "" {
			text = tok.String()
		}
		p := fset.Position(pos)
		out = append(out, Token{
			Kind:     tok.String(),
			Text:     text,
			Offset:   p.Offset,
			Line:     p.Line,
			Keyword:  tok.IsKeyword(),
			Literal:  tok.IsLiteral(),
			Operator: tok.IsOperator(),
		})
	}
	return out, nil
}

// Imports lists the packages the source imports, sorted and deduplicated.
func Imports(source string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "input.go", source, parser.ImportsOnly)
	if err != nil {
		return nil, fmt.Errorf("cannot parse: %w", err)
	}
	seen := map[string]bool{}
	out := []string{}
	for _, imp := range f.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
}
