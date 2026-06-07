// Package numbers formats and summarises numeric data.
//
// Like its two sibling packages it exports New and Options. Nothing here knows
// about the others; the namespaces are added by gowasm at the boundary.
package numbers

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Options controls formatting.
type Options struct {
	// Decimals is how many digits follow the point.
	Decimals int `json:"decimals"`
	// Separator groups thousands. Empty leaves the digits unbroken.
	Separator string `json:"separator,omitempty"`
	Suffix    string `json:"suffix,omitempty"`
}

// Summary is what Stats returns.
type Summary struct {
	Count int     `json:"count"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Mean  float64 `json:"mean"`
}

// New returns the default options for a given precision.
func New(decimals int) Options {
	if decimals < 0 {
		decimals = 0
	}
	return Options{Decimals: decimals, Separator: ","}
}

// Format renders v according to opts.
func Format(v float64, opts Options) string {
	if opts.Decimals < 0 {
		opts.Decimals = 0
	}
	s := strconv.FormatFloat(v, 'f', opts.Decimals, 64)

	if opts.Separator != "" {
		sign := ""
		if strings.HasPrefix(s, "-") {
			sign, s = "-", s[1:]
		}
		whole, frac, hasFrac := strings.Cut(s, ".")
		var parts []string
		for len(whole) > 3 {
			parts = append([]string{whole[len(whole)-3:]}, parts...)
			whole = whole[:len(whole)-3]
		}
		parts = append([]string{whole}, parts...)
		s = sign + strings.Join(parts, opts.Separator)
		if hasFrac {
			s += "." + frac
		}
	}
	return s + opts.Suffix
}

// Stats summarises a series. An empty series is an error rather than a zeroed
// Summary, which would be indistinguishable from a series of zeros.
func Stats(values []float64) (Summary, error) {
	if len(values) == 0 {
		return Summary{}, fmt.Errorf("no values to summarise")
	}
	s := Summary{Count: len(values), Min: math.Inf(1), Max: math.Inf(-1)}
	total := 0.0
	for _, v := range values {
		s.Min = math.Min(s.Min, v)
		s.Max = math.Max(s.Max, v)
		total += v
	}
	s.Mean = total / float64(len(values))
	return s, nil
}
