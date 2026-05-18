// Package highlight tokenises source code in any of Chroma's languages.
//
// The JavaScript options are Prism, highlight.js and Shiki. Prism and
// highlight.js maintain their own grammars; Shiki is excellent but carries a
// full TextMate engine and its grammars are large JSON files loaded per
// language.
//
// Chroma is a port of Pygments' lexers, so it arrives with well over two
// hundred languages already compiled in, and picking one costs nothing extra at
// load time. That trade -- a larger module once, no per-language fetch after --
// is often the right one for an editor that has to highlight whatever a user
// pastes in.
//
// Tokens are returned rather than HTML. A caller who wants HTML can build it,
// and a caller who wants to feed a canvas, a diff view or a screen reader is
// not forced to parse markup back out again.
package highlight

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// Token is one lexed fragment.
type Token struct {
	// Kind is the token category, such as "Keyword" or "LiteralString".
	Kind string `json:"kind"`
	Text string `json:"text"`
	// Color is the hex colour the chosen style gives this token, when it gives
	// one. Empty means inherit.
	Color string `json:"color,omitempty"`
	Bold  bool   `json:"bold,omitempty"`
	Italic bool  `json:"italic,omitempty"`
}

// Detection is a guessed language, with how the guess was made.
type Detection struct {
	Language Language `json:"language"`
	// By is "filename" or "content". Filename matching is reliable; content
	// analysis is heuristic and frequently wrong, so it is reported separately
	// rather than presented as if the two were equivalent.
	By string `json:"by"`
}

// Language describes one of the available lexers.
type Language struct {
	Name      string   `json:"name"`
	Aliases   []string `json:"aliases"`
	Filenames []string `json:"filenames"`
	MimeTypes []string `json:"mimeTypes"`
}

// Result is highlighted source.
type Result struct {
	Language string  `json:"language"`
	Style    string  `json:"style"`
	Tokens   []Token `json:"tokens"`
	// Background is the style's page colour, so a caller can match it.
	Background string `json:"background,omitempty"`
}

func lexerFor(name string) (chroma.Lexer, error) {
	l := lexers.Get(name)
	if l == nil {
		return nil, fmt.Errorf("no lexer for %q; call languages to see what is available", name)
	}
	return chroma.Coalesce(l), nil
}

// Languages lists every available lexer.
func Languages() ([]Language, error) {
	out := []Language{}
	for _, l := range lexers.GlobalLexerRegistry.Lexers {
		out = append(out, describe(l))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Styles lists the available colour schemes.
func Styles() ([]string, error) {
	out := append([]string{}, styles.Names()...)
	sort.Strings(out)
	return out, nil
}

// Detect identifies the language of a fragment.
//
// A filename is matched against each lexer's patterns, which is reliable. With
// no filename it falls back to analysing the content, which is a heuristic and
// often wrong or inconclusive, so the result says which was used. Prefer
// passing a filename when you have one.
func Detect(filename, source string) (Detection, error) {
	if filename != "" {
		if l := lexers.Match(filename); l != nil {
			return Detection{Language: describe(l), By: "filename"}, nil
		}
	}
	if l := lexers.Analyse(source); l != nil {
		return Detection{Language: describe(l), By: "content"}, nil
	}
	return Detection{}, fmt.Errorf("could not identify the language")
}

func describe(l chroma.Lexer) Language {
	c := l.Config()
	return Language{
		Name:      c.Name,
		Aliases:   append([]string{}, c.Aliases...),
		Filenames: append([]string{}, c.Filenames...),
		MimeTypes: append([]string{}, c.MimeTypes...),
	}
}

// Tokenize lexes source in the named language, colouring each token with the
// named style.
func Tokenize(language, style, source string) (Result, error) {
	lexer, err := lexerFor(language)
	if err != nil {
		return Result{}, err
	}
	// styles.Get returns a fallback rather than nil for an unknown name, which
	// would silently colour the output with the wrong scheme.
	s, ok := styles.Registry[style]
	if !ok {
		return Result{}, fmt.Errorf("no style named %q; call styles to see what is available", style)
	}

	it, err := lexer.Tokenise(nil, source)
	if err != nil {
		return Result{}, fmt.Errorf("lexing as %s: %w", language, err)
	}

	tokens := []Token{}
	for _, t := range it.Tokens() {
		entry := s.Get(t.Type)
		tok := Token{Kind: t.Type.String(), Text: t.Value}
		if entry.Colour.IsSet() {
			tok.Color = entry.Colour.String()
		}
		tok.Bold = entry.Bold == chroma.Yes
		tok.Italic = entry.Italic == chroma.Yes
		tokens = append(tokens, tok)
	}

	bg := ""
	if c := s.Get(chroma.Background).Background; c.IsSet() {
		bg = c.String()
	}
	return Result{
		Language:   lexer.Config().Name,
		Style:      s.Name,
		Tokens:     tokens,
		Background: bg,
	}, nil
}

// Kinds lists the distinct token categories in a fragment, which is what you
// need to write a stylesheet for it.
func Kinds(language, source string) ([]string, error) {
	lexer, err := lexerFor(language)
	if err != nil {
		return nil, err
	}
	it, err := lexer.Tokenise(nil, source)
	if err != nil {
		return nil, fmt.Errorf("lexing as %s: %w", language, err)
	}

	seen := map[string]bool{}
	out := []string{}
	for _, t := range it.Tokens() {
		k := t.Type.String()
		if !seen[k] && strings.TrimSpace(t.Value) != "" {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}
