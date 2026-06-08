// Package valueuse has a type with methods that is only ever used by value, so
// it stays data and its methods are simply not reachable.
package valueuse

type Amount struct {
	Cents int `json:"cents"`
}

func (a *Amount) Add(n int) { a.Cents += n }

func Parse(s string) (Amount, error) { return Amount{}, nil }
