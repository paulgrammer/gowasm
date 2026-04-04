// Package urls extracts and inspects URLs found in free text.
//
// It is the gowasm example package: every exported function here becomes a
// TypeScript function in the generated npm package, with no annotations.
package urls

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"mvdan.cc/xurls/v2"
)

// Strictness selects how eagerly text is scanned for URLs.
type Strictness string

const (
	// Relaxed also matches URLs written without a scheme, like "go.dev".
	Relaxed Strictness = "relaxed"
	// Strict matches only URLs with an explicit scheme.
	Strict Strictness = "strict"
)

// Match is a single URL found in the input.
type Match struct {
	// Raw is the URL exactly as it appeared in the text.
	Raw string `json:"raw"`
	// Scheme is absent for schemeless matches found in relaxed mode.
	Scheme string `json:"scheme,omitempty"`
	Host   string `json:"host"`
	// Offset is the byte index of the match within the input.
	Offset int `json:"offset"`
}

// ExtractURLs finds every URL in text.
func ExtractURLs(text string, mode Strictness) ([]Match, error) {
	var re interface{ FindAllString(string, int) []string }
	switch mode {
	case Relaxed:
		re = xurls.Relaxed()
	case Strict:
		re = xurls.Strict()
	default:
		return nil, fmt.Errorf("unknown strictness %q, want %q or %q", mode, Relaxed, Strict)
	}

	found := re.FindAllString(text, -1)
	out := make([]Match, 0, len(found))
	for _, raw := range found {
		m := Match{Raw: raw, Offset: strings.Index(text, raw)}
		// Relaxed matches may have no scheme, which url.Parse reads as a path.
		probe := raw
		if !strings.Contains(probe, "://") {
			probe = "https://" + probe
		} else {
			m.Scheme = raw[:strings.Index(raw, "://")]
		}
		if u, err := url.Parse(probe); err == nil {
			m.Host = u.Host
		}
		out = append(out, m)
	}
	return out, nil
}

// FirstHost returns the host of the first URL in text, or nil when there is none.
func FirstHost(text string) (*Match, error) {
	matches, err := ExtractURLs(text, Relaxed)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}
	return &matches[0], nil
}

// Sum adds every number, demonstrating slice parameters.
func Sum(nums []int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}

// Hash returns the hex-encoded SHA-256 of data. []byte crosses the boundary as
// a base64 string, matching encoding/json.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ParseWhen parses an RFC 3339 timestamp, rejecting on malformed input.
func ParseWhen(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse %q: %w", s, err)
	}
	return t.UTC(), nil
}

// Counts reports how many URLs and how many distinct hosts appear in text,
// demonstrating multiple return values.
func Counts(text string) (int, int, error) {
	matches, err := ExtractURLs(text, Relaxed)
	if err != nil {
		return 0, 0, err
	}
	hosts := map[string]bool{}
	for _, m := range matches {
		hosts[m.Host] = true
	}
	return len(matches), len(hosts), nil
}

// Tally groups matches by host, demonstrating map results.
func Tally(text string) (map[string]int, error) {
	matches, err := ExtractURLs(text, Relaxed)
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	for _, m := range matches {
		out[m.Host]++
	}
	return out, nil
}

// Ping does nothing and returns nothing; it exists to prove void calls work.
func Ping() {}
