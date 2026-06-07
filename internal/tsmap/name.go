// Package tsmap converts Go types and identifiers into their TypeScript
// equivalents.
package tsmap

import (
	"strings"
	"unicode"
)

// CamelCase lowercases the leading uppercase run of a Go identifier, keeping
// acronyms readable:
//
//	GetPosts    -> getPosts
//	ID          -> id
//	HTMLParser  -> htmlParser
//	ExtractURLs -> extractURLs
//	A           -> a
//	already     -> already
//
// The rule is: a run of length 1 lowercases just that character; a fully
// uppercase identifier lowercases entirely; otherwise every character of the
// run except the last one is lowercased, because that last one begins the next
// word.
func CamelCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)

	n := 0
	for n < len(r) && unicode.IsUpper(r[n]) {
		n++
	}

	switch {
	case n == 0:
		return s
	case n == 1:
		r[0] = unicode.ToLower(r[0])
	case n == len(r):
		for i := range r {
			r[i] = unicode.ToLower(r[i])
		}
	default:
		for i := 0; i < n-1; i++ {
			r[i] = unicode.ToLower(r[i])
		}
	}
	return string(r)
}

// JSONFieldName reports the name encoding/json will use for a struct field,
// and whether the field is omitted when empty.
//
// The fallback matters: with no json tag encoding/json emits the Go field name
// *verbatim*, so camelCasing it here would make the generated TypeScript
// disagree with the actual payload.
func JSONFieldName(goName string, lookup func(string) (string, bool)) (name string, optional bool, skip bool) {
	raw, ok := lookup("json")
	if !ok {
		return goName, false, false
	}
	parts := strings.Split(raw, ",")
	if parts[0] == "-" && len(parts) == 1 {
		return "", false, true
	}
	name = parts[0]
	if name == "" {
		name = goName
	}
	for _, opt := range parts[1:] {
		// Matched against the json tag's own options only. Checking the whole
		// struct tag for "omitempty" would misfire on tags like
		// `validate:"omitempty"`, which say nothing about JSON encoding.
		if opt == "omitempty" || opt == "omitzero" {
			optional = true
		}
	}
	return name, optional, false
}

// ReservedJS lists the words that cannot be a variable declaration name in a
// JavaScript module. Most are reserved words; eval and arguments are not, but
// are equally restricted under strict mode, which modules always are.
//
// All of them are legal as object properties, so a Go function named New still
// works on the client object. Only the bare named export is impossible, and
// such names are far too common in Go to reject outright.
var ReservedJS = map[string]bool{
	"await": true, "break": true, "case": true, "catch": true, "class": true,
	"const": true, "continue": true, "debugger": true, "default": true,
	"delete": true, "do": true, "else": true, "enum": true, "export": true,
	"extends": true, "false": true, "finally": true, "for": true,
	"function": true, "if": true, "implements": true, "import": true,
	"in": true, "instanceof": true, "interface": true, "let": true,
	"new": true, "null": true, "package": true, "private": true,
	"protected": true, "public": true, "return": true, "static": true,
	"super": true, "switch": true, "this": true, "throw": true, "true": true,
	"try": true, "typeof": true, "var": true, "void": true, "while": true,
	"with": true, "yield": true,
	// Not reserved words, but binding either is a strict mode error.
	"eval": true, "arguments": true,
}

// Identifier reduces a name to something legal as a TypeScript identifier,
// keeping its case: "go-qrcode" -> "goqrcode", "2fast" -> "pkg2fast".
func Identifier(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_', r == '$':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		out = "pkg" + out
	}
	return out
}

// IsIdentifier reports whether s is already a legal, unreserved TypeScript
// identifier.
func IsIdentifier(s string) bool {
	if s == "" || ReservedJS[s] {
		return false
	}
	for i, r := range s {
		switch {
		case unicode.IsLetter(r), r == '_', r == '$':
		case unicode.IsDigit(r) && i > 0:
		default:
			return false
		}
	}
	return true
}
