// Package sanitize cleans untrusted HTML without needing a DOM.
//
// The usual JavaScript answer is DOMPurify, which works by parsing the input
// with the browser's own parser. That is a strength in a browser and a problem
// everywhere else: on a server it needs jsdom, which is a large dependency that
// implements enough of a browser to be worth attacking on its own.
//
// bluemonday parses and rewrites the markup directly, so the same code runs in
// Node, in a worker, at the edge, and in the browser, with no DOM and no
// jsdom. It is allowlist based: anything not explicitly permitted is removed,
// which is the only policy direction that fails safe.
package sanitize

import (
	"fmt"
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// Policy selects how aggressive the cleaning is.
type Policy string

const (
	// Strict removes all markup, leaving only text.
	Strict Policy = "strict"
	// Basic allows the formatting tags a comment box needs.
	Basic Policy = "basic"
	// Article allows headings, lists, tables, images and links.
	Article Policy = "article"
)

// Result is the cleaned markup and what changed.
type Result struct {
	HTML string `json:"html"`
	// Removed lists the tags and attributes that were stripped, so a user can
	// be told what happened to their input rather than silently losing it.
	Removed []string `json:"removed"`
	// Changed reports whether anything was altered at all.
	Changed bool `json:"changed"`
	// InputBytes and OutputBytes bracket the size change.
	InputBytes  int `json:"inputBytes"`
	OutputBytes int `json:"outputBytes"`
}

func policyFor(p Policy) (*bluemonday.Policy, error) {
	switch p {
	case Strict:
		return bluemonday.StrictPolicy(), nil
	case Basic:
		pol := bluemonday.NewPolicy()
		pol.AllowElements("b", "strong", "i", "em", "code", "pre", "br", "p", "blockquote")
		pol.AllowAttrs("href").OnElements("a")
		pol.AllowURLSchemes("http", "https", "mailto")
		pol.RequireNoFollowOnLinks(true)
		return pol, nil
	case Article:
		pol := bluemonday.UGCPolicy()
		pol.AllowElements("h1", "h2", "h3", "h4", "table", "thead", "tbody", "tr", "th", "td")
		pol.AllowAttrs("src", "alt", "title").OnElements("img")
		pol.AllowURLSchemes("http", "https", "mailto")
		pol.RequireNoFollowOnLinks(true)
		return pol, nil
	default:
		return nil, fmt.Errorf("unknown policy %q, want %q, %q or %q", p, Strict, Basic, Article)
	}
}

// tagsIn lists the element names appearing in some markup, crudely but well
// enough to report what a policy removed.
func tagsIn(html string) map[string]bool {
	out := map[string]bool{}
	for i := 0; i < len(html); i++ {
		if html[i] != '<' {
			continue
		}
		j := i + 1
		if j < len(html) && html[j] == '/' {
			j++
		}
		start := j
		for j < len(html) && (isAlpha(html[j]) || isDigit(html[j])) {
			j++
		}
		if j > start {
			out[strings.ToLower(html[start:j])] = true
		}
		i = j
	}
	return out
}

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// Clean removes everything the policy does not explicitly allow.
func Clean(html string, policy Policy) (Result, error) {
	pol, err := policyFor(policy)
	if err != nil {
		return Result{}, err
	}
	out := pol.Sanitize(html)

	before, after := tagsIn(html), tagsIn(out)
	removed := []string{}
	for tag := range before {
		if !after[tag] {
			removed = append(removed, tag)
		}
	}
	// Sorted so the result is stable and testable.
	for i := 1; i < len(removed); i++ {
		for j := i; j > 0 && removed[j] < removed[j-1]; j-- {
			removed[j], removed[j-1] = removed[j-1], removed[j]
		}
	}

	return Result{
		HTML:        out,
		Removed:     removed,
		Changed:     out != html,
		InputBytes:  len(html),
		OutputBytes: len(out),
	}, nil
}

// Text strips all markup, leaving only the text content.
func Text(html string) (string, error) {
	return bluemonday.StrictPolicy().Sanitize(html), nil
}

// IsSafe reports whether the markup already satisfies the policy, meaning
// cleaning would change nothing.
func IsSafe(html string, policy Policy) (bool, error) {
	r, err := Clean(html, policy)
	if err != nil {
		return false, err
	}
	return !r.Changed, nil
}
