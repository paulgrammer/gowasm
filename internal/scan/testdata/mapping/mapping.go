// Package mapping exercises every type rule the generator supports. It is
// testdata, so it is never built as part of the module.
package mapping

import "time"

type Status string

const (
	Active   Status = "active"
	Archived Status = "archived"
)

type Priority int

const (
	Low  Priority = 1
	High Priority = 9
)

// Untagged is a plain alias with no constants, so it keeps its name.
type Untagged string

type Inner struct {
	Value string `json:"value"`
}

type Outer struct {
	Inner                // embedded, no tag: encoded inline
	Name       string    // no json tag: encoding/json emits the Go name verbatim
	Renamed    string    `json:"renamed"`
	Optional   string    `json:"optional,omitempty"`
	Ptr        *Inner    `json:"ptr"`
	Blob       []byte    `json:"blob"`
	When       time.Time `json:"when"`
	Big        int64     `json:"big"`
	Ignored    string    `json:"-"`
	unexported string
	Anything   any            `json:"anything"`
	Counts     map[string]int `json:"counts"`
	Children   []Inner        `json:"children"`
	SelfRef    *Outer         `json:"selfRef"`
}

func Basics(s string, b bool, i int, f float64) string { return s }

func Enums(st Status, p Priority) Status { return st }

func Aliased(u Untagged) Untagged { return u }

func Nested(o Outer) (Outer, error) { return o, nil }

func Bytes(data []byte) []byte { return data }

func Tuple(s string) (int, string, error) { return 0, s, nil }

func Nothing() {}

func OnlyError() error { return nil }

func Variadic(prefix string, rest ...int) string { return prefix }

func Pointers(in *Inner) (*Outer, error) { return nil, nil }

func Maps(m map[string]Inner) map[string][]byte { return nil }

func Anything(v any) any { return v }
