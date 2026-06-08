// Package stringer has a type whose only exported method is conventional.
// Adding a String() must not turn data into an opaque handle.
package stringer

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func (p *Point) String() string { return "point" }

func Origin() *Point     { return &Point{} }
func Move(p Point) Point { return p }
