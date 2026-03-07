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
