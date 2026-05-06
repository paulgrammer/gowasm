// Package money does arithmetic that JavaScript numbers cannot do correctly.
//
// JavaScript has one numeric type, IEEE-754 double. 0.1 + 0.2 is 0.30000000000000004,
// and 2^53 + 1 is not representable at all. Neither is acceptable for money:
// the first loses cents to rounding, the second silently truncates identifiers
// and large amounts.
//
// This package keeps amounts as integer minor units -- cents, not dollars --
// so every operation is exact. Division states its remainder rather than
// discarding it, and splitting a bill distributes the odd penny instead of
// quietly losing it.
package money

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Currency is one of the supported currencies.
type Currency string

const (
	USD Currency = "USD"
	EUR Currency = "EUR"
	GBP Currency = "GBP"
	JPY Currency = "JPY"
	KES Currency = "KES"
	UGX Currency = "UGX"
)

// exponent is how many decimal places each currency has. Yen and shillings
// have none, which is exactly the sort of thing a hardcoded /100 gets wrong.
var exponent = map[Currency]int{USD: 2, EUR: 2, GBP: 2, JPY: 0, KES: 2, UGX: 0}

var symbol = map[Currency]string{USD: "$", EUR: "€", GBP: "£", JPY: "¥", KES: "KSh", UGX: "USh"}

// Amount is an exact monetary value, held as integer minor units.
//
// Minor is a string rather than a number because it is the one field that must
// survive the boundary intact: beyond 2^53 a JavaScript number would round it,
// and rounding money is the bug this package exists to prevent.
type Amount struct {
	Minor    string   `json:"minor"`
	Currency Currency `json:"currency"`
	// Display is the human-readable form, formatted for the currency.
	Display string `json:"display"`
}

// Division is the result of splitting an amount, with the remainder stated.
type Division struct {
	Shares    []Amount `json:"shares"`
	Remainder Amount   `json:"remainder"`
}

func places(c Currency) (int, error) {
	e, ok := exponent[c]
	if !ok {
		return 0, fmt.Errorf("unknown currency %q", c)
	}
	return e, nil
}

func newAmount(minor int64, c Currency) (Amount, error) {
	e, err := places(c)
	if err != nil {
		return Amount{}, err
	}
	return Amount{
		Minor:    strconv.FormatInt(minor, 10),
		Currency: c,
		Display:  format(minor, e, symbol[c]),
	}, nil
}

func format(minor int64, places int, sym string) string {
	neg := minor < 0
	if neg {
		minor = -minor
	}
	s := strconv.FormatInt(minor, 10)
	if places > 0 {
		for len(s) <= places {
			s = "0" + s
		}
		s = s[:len(s)-places] + "." + s[len(s)-places:]
	}
	// Group the integer part in threes.
	dot := strings.IndexByte(s, '.')
	intPart, frac := s, ""
	if dot >= 0 {
		intPart, frac = s[:dot], s[dot:]
	}
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := sym + b.String() + frac
	if neg {
		out = "-" + out
	}
	return out
}

func (a Amount) minor() (int64, error) {
	v, err := strconv.ParseInt(a.Minor, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q is not an integer number of minor units", a.Minor)
	}
	return v, nil
}

func same(a, b Amount) error {
	if a.Currency != b.Currency {
		return fmt.Errorf("cannot combine %s and %s", a.Currency, b.Currency)
	}
	return nil
}

// Parse reads a decimal string such as "19.99" into an exact amount.
//
// It works on the text, never on a float, so no value is rounded on the way in.
func Parse(value string, c Currency) (Amount, error) {
	e, err := places(c)
	if err != nil {
		return Amount{}, err
	}

	s := strings.TrimSpace(value)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "-"), "+")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" {
		return Amount{}, fmt.Errorf("empty amount")
	}

	intPart, frac, _ := strings.Cut(s, ".")
	if len(frac) > e {
		return Amount{}, fmt.Errorf("%s has %d decimal places, but %s has %d", value, len(frac), c, e)
	}
	for len(frac) < e {
		frac += "0"
	}

	digits := intPart + frac
	for _, r := range digits {
		if r < '0' || r > '9' {
			return Amount{}, fmt.Errorf("%q is not a number", value)
		}
	}
	minor, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return Amount{}, fmt.Errorf("%s does not fit in 64 bits", value)
	}
	if neg {
		minor = -minor
	}
	return newAmount(minor, c)
}

// Add sums two amounts of the same currency.
func Add(a, b Amount) (Amount, error) {
	if err := same(a, b); err != nil {
		return Amount{}, err
	}
	x, err := a.minor()
	if err != nil {
		return Amount{}, err
	}
	y, err := b.minor()
	if err != nil {
		return Amount{}, err
	}
	if (y > 0 && x > math.MaxInt64-y) || (y < 0 && x < math.MinInt64-y) {
		return Amount{}, fmt.Errorf("sum overflows 64 bits")
	}
	return newAmount(x+y, a.Currency)
}

// Subtract takes b from a.
func Subtract(a, b Amount) (Amount, error) {
	if err := same(a, b); err != nil {
		return Amount{}, err
	}
	x, err := a.minor()
	if err != nil {
		return Amount{}, err
	}
	y, err := b.minor()
	if err != nil {
		return Amount{}, err
	}
	return newAmount(x-y, a.Currency)
}

// Sum adds any number of amounts, demonstrating a variadic parameter.
func Sum(amounts ...Amount) (Amount, error) {
	if len(amounts) == 0 {
		return Amount{}, fmt.Errorf("nothing to sum")
	}
	total := amounts[0]
	for _, a := range amounts[1:] {
		var err error
		if total, err = Add(total, a); err != nil {
			return Amount{}, err
		}
	}
	return total, nil
}

// Split divides an amount into n shares, distributing any remainder one minor
// unit at a time so the shares add back up to exactly the original.
//
// A naive division loses money: 10.00 into 3 gives 3.33 three times, and one
// cent vanishes. Here the first share carries it.
func Split(a Amount, n int) (Division, error) {
	if n < 1 {
		return Division{}, fmt.Errorf("cannot split into %d shares", n)
	}
	total, err := a.minor()
	if err != nil {
		return Division{}, err
	}

	base, rem := total/int64(n), total%int64(n)
	shares := make([]Amount, 0, n)
	for i := range n {
		v := base
		if int64(i) < rem {
			v++
		}
		s, err := newAmount(v, a.Currency)
		if err != nil {
			return Division{}, err
		}
		shares = append(shares, s)
	}
	zero, err := newAmount(0, a.Currency)
	if err != nil {
		return Division{}, err
	}
	return Division{Shares: shares, Remainder: zero}, nil
}

// ApplyRate multiplies by a rate given as a decimal string, rounding half away
// from zero, which is what accounting conventions expect.
func ApplyRate(a Amount, rate string) (Amount, error) {
	minor, err := a.minor()
	if err != nil {
		return Amount{}, err
	}
	// The rate is scaled to an integer so the multiplication stays exact.
	intPart, frac, _ := strings.Cut(strings.TrimSpace(rate), ".")
	digits := intPart + frac
	scaled, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return Amount{}, fmt.Errorf("rate %q is not a number", rate)
	}
	scale := int64(1)
	for range len(frac) {
		scale *= 10
	}

	product := minor * scaled
	q := product / scale
	if r := product % scale; r*2 >= scale {
		q++
	} else if r*2 <= -scale {
		q--
	}
	return newAmount(q, a.Currency)
}

// Compare orders two amounts: -1, 0 or 1.
func Compare(a, b Amount) (int, error) {
	if err := same(a, b); err != nil {
		return 0, err
	}
	x, err := a.minor()
	if err != nil {
		return 0, err
	}
	y, err := b.minor()
	if err != nil {
		return 0, err
	}
	switch {
	case x < y:
		return -1, nil
	case x > y:
		return 1, nil
	default:
		return 0, nil
	}
}
