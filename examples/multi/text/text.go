// Package text formats prose.
//
// It is one of three packages this example exposes, and all three export New
// and Options. Under a flat scheme two of them would have to be renamed; here
// each keeps the name Go gave it and lives under its own namespace, so this is
// text.new() and text.Options in TypeScript.
package text

import (
	"fmt"
	"strings"
	"unicode"
)

// Casing selects how Convert rewrites a string. Constants of a named type
// become a TypeScript literal union, so a typo is a compile error.
type Casing string

const (
	Upper Casing = "upper"
	Lower Casing = "lower"
	Title Casing = "title"
)

// Options controls wrapping.
type Options struct {
	// Width is the column to wrap at, counted in runes.
	Width  int    `json:"width"`
	Indent string `json:"indent,omitempty"`
}

// New returns the default options for a given width.
func New(width int) Options {
	if width <= 0 {
		width = 72
	}
	return Options{Width: width}
}

// Wrap breaks s into lines no wider than the configured width.
func Wrap(s string, opts Options) []string {
	if opts.Width <= 0 {
		opts.Width = 72
	}
	var lines []string
	var line []string
	width := 0
	for _, word := range strings.Fields(s) {
		n := len([]rune(word))
		if width > 0 && width+1+n > opts.Width {
			lines = append(lines, opts.Indent+strings.Join(line, " "))
			line, width = nil, 0
		}
		if width > 0 {
			width++
		}
		line = append(line, word)
		width += n
	}
	if len(line) > 0 {
		lines = append(lines, opts.Indent+strings.Join(line, " "))
	}
	return lines
}

// Convert rewrites s in the given casing.
func Convert(s string, casing Casing) (string, error) {
	switch casing {
	case Upper:
		return strings.ToUpper(s), nil
	case Lower:
		return strings.ToLower(s), nil
	case Title:
		return title(s), nil
	default:
		return "", fmt.Errorf("unknown casing %q", casing)
	}
}

func title(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}
