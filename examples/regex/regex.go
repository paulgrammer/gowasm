// Package regex exposes Go's regexp engine, which cannot backtrack.
//
// JavaScript's regular expressions are backtracking, so a pattern like
// (a+)+$ against a long run of "a" takes exponential time and hangs the event
// loop. That is the ReDoS class of denial of service, and it is why accepting a
// regular expression from a user is normally unsafe.
//
// Go's regexp is RE2: it runs in time linear in the length of the input,
// regardless of the pattern. There is no input that makes it hang. That makes
// this the rare case where compiling Go to WebAssembly gives JavaScript a
// capability it does not otherwise have, rather than merely a second way to do
// something it can already do.
//
// The trade is that RE2 has no backreferences and no lookaround, because those
// are what force backtracking. Patterns using them are rejected at compile
// time rather than accepted and made slow.
package regex

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Group is one capture inside a match. Unnamed groups have an empty Name.
type Group struct {
	Index int    `json:"index"`
	Name  string `json:"name,omitempty"`
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// Match is one occurrence of a pattern, with byte offsets into the input.
type Match struct {
	Text   string  `json:"text"`
	Start  int     `json:"start"`
	End    int     `json:"end"`
	Groups []Group `json:"groups"`
}

// Pattern describes a compiled expression.
type Pattern struct {
	Source string `json:"source"`
	// Groups is the number of capturing groups.
	Groups int `json:"groups"`
	// Names lists the named capture groups in order.
	Names []string `json:"names"`
	// Literal is the prefix every match must begin with, when the pattern has
	// one. Useful for explaining why a pattern is or is not selective.
	Literal string `json:"literal,omitempty"`
}

// Timing reports how long a match took, which is how the linear-time claim can
// be checked rather than believed.
type Timing struct {
	Matched      bool   `json:"matched"`
	InputLength  int    `json:"inputLength"`
	Microseconds int64  `json:"microseconds"`
	Note         string `json:"note,omitempty"`
}

func compile(pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		// RE2 rejects backreferences and lookaround outright. Saying so is more
		// useful than passing along "invalid or unsupported Perl syntax".
		msg := err.Error()
		switch {
		case strings.Contains(msg, "invalid or unsupported Perl syntax"):
			return nil, fmt.Errorf("%s: this engine has no lookaround or backreferences, which is what makes it immune to catastrophic backtracking", pattern)
		default:
			return nil, fmt.Errorf("compiling %q: %w", pattern, err)
		}
	}
	return re, nil
}

// Describe compiles a pattern and reports its structure without running it.
func Describe(pattern string) (Pattern, error) {
	re, err := compile(pattern)
	if err != nil {
		return Pattern{}, err
	}
	names := []string{}
	for _, n := range re.SubexpNames() {
		if n != "" {
			names = append(names, n)
		}
	}
	prefix, _ := re.LiteralPrefix()
	return Pattern{
		Source:  re.String(),
		Groups:  re.NumSubexp(),
		Names:   names,
		Literal: prefix,
	}, nil
}

// Test reports whether the pattern matches anywhere in the input.
func Test(pattern, input string) (bool, error) {
	re, err := compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(input), nil
}

// FindAll returns every match, with capture groups and byte offsets.
func FindAll(pattern, input string) ([]Match, error) {
	re, err := compile(pattern)
	if err != nil {
		return nil, err
	}
	names := re.SubexpNames()

	out := []Match{}
	for _, loc := range re.FindAllStringSubmatchIndex(input, -1) {
		m := Match{Text: input[loc[0]:loc[1]], Start: loc[0], End: loc[1], Groups: []Group{}}
		for g := 1; g*2 < len(loc); g++ {
			start, end := loc[g*2], loc[g*2+1]
			if start < 0 {
				// The group did not participate in this match.
				continue
			}
			m.Groups = append(m.Groups, Group{
				Index: g,
				Name:  names[g],
				Text:  input[start:end],
				Start: start,
				End:   end,
			})
		}
		out = append(out, m)
	}
	return out, nil
}

// Replace substitutes every match. $1 and ${name} refer to capture groups.
func Replace(pattern, input, replacement string) (string, error) {
	re, err := compile(pattern)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(input, replacement), nil
}

// Split cuts the input around every match. A limit below zero means no limit.
func Split(pattern, input string, limit int) ([]string, error) {
	re, err := compile(pattern)
	if err != nil {
		return nil, err
	}
	return re.Split(input, limit), nil
}

// TimeMatch runs a pattern and reports how long it took.
//
// The point of this function is the input JavaScript cannot survive. Try
// pattern "(a+)+$" against forty "a" characters followed by a "b": V8 takes
// exponential time and blocks the event loop, while this returns in
// microseconds, because RE2 has no backtracking to blow up.
func TimeMatch(pattern, input string) (Timing, error) {
	re, err := compile(pattern)
	if err != nil {
		return Timing{}, err
	}

	start := time.Now()
	matched := re.MatchString(input)
	elapsed := time.Since(start)

	t := Timing{
		Matched:      matched,
		InputLength:  len(input),
		Microseconds: elapsed.Microseconds(),
	}
	if strings.Contains(pattern, "+)+") || strings.Contains(pattern, "*)*") {
		t.Note = "this shape of pattern is the classic ReDoS trigger; a backtracking engine would still be running"
	}
	return t, nil
}
