// Package cue validates configuration against a CUE schema.
//
// There is no JavaScript implementation of CUE. Not a weaker one, none. So
// unlike most of these examples, this is not "Go's library is better" but
// "this language does not otherwise run here at all".
//
// What CUE gives you over JSON Schema is that types and values are the same
// thing. A schema is a CUE value, a config is a CUE value, and validating is
// unifying the two. Constraints compose by intersection, so a base schema and
// an environment-specific override combine without a merge strategy, and a
// contradiction between them is an error rather than a last-writer-wins
// surprise.
package cue

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/json"
)

// Violation is one constraint failure, with where it happened.
type Violation struct {
	// Path is the dotted path to the offending field, such as "server.port".
	Path    string `json:"path"`
	Message string `json:"message"`
	// Line and Column locate it in the input, when known.
	Line   int `json:"line,omitempty"`
	Column int `json:"column,omitempty"`
}

// Report is the outcome of validating a value against a schema.
type Report struct {
	Valid      bool        `json:"valid"`
	Violations []Violation `json:"violations"`
	// Concrete is the fully resolved value when validation passed, with
	// defaults filled in. This is the part JSON Schema cannot do: the schema
	// carries the defaults, so validating also completes the config.
	Concrete string `json:"concrete,omitempty"`
}

func newContext() *cue.Context { return cuecontext.New() }

func violations(err error) []Violation {
	out := []Violation{}
	for _, e := range errors.Errors(err) {
		v := Violation{
			Path:    strings.Join(e.Path(), "."),
			Message: e.Error(),
		}
		if pos := e.Position(); pos.IsValid() {
			v.Line = pos.Line()
			v.Column = pos.Column()
		}
		out = append(out, v)
	}
	return out
}

// Check compiles a schema on its own, so a broken schema is reported as such
// rather than as every config failing to match it.
func Check(schema string) ([]Violation, error) {
	ctx := newContext()
	v := ctx.CompileString(schema)
	if err := v.Err(); err != nil {
		return violations(err), nil
	}
	return []Violation{}, nil
}

// Validate checks a JSON document against a CUE schema.
func Validate(schema, jsonData string) (Report, error) {
	ctx := newContext()

	s := ctx.CompileString(schema)
	if err := s.Err(); err != nil {
		return Report{}, fmt.Errorf("the schema does not compile: %w", err)
	}

	expr, err := json.Extract("input.json", []byte(jsonData))
	if err != nil {
		return Report{}, fmt.Errorf("the input is not valid JSON: %w", err)
	}
	d := ctx.BuildExpr(expr)
	if err := d.Err(); err != nil {
		return Report{}, fmt.Errorf("cannot read the input: %w", err)
	}

	// Unification is the whole operation: the result holds everything both
	// sides say, and is an error if they disagree.
	unified := s.Unify(d)
	if err := unified.Validate(cue.Concrete(true)); err != nil {
		return Report{Valid: false, Violations: violations(err)}, nil
	}

	out, err := unified.MarshalJSON()
	if err != nil {
		return Report{}, fmt.Errorf("cannot render the result: %w", err)
	}
	return Report{Valid: true, Violations: []Violation{}, Concrete: string(out)}, nil
}

// Unify combines several CUE values into one.
//
// This is how a base configuration and an environment override are composed.
// Order does not matter, and a genuine conflict is an error rather than one
// side silently winning.
func Unify(values []string) (string, error) {
	if len(values) == 0 {
		return "", fmt.Errorf("nothing to unify")
	}
	ctx := newContext()

	result := ctx.CompileString(values[0])
	if err := result.Err(); err != nil {
		return "", fmt.Errorf("value 1 does not compile: %w", err)
	}
	for i, src := range values[1:] {
		v := ctx.CompileString(src)
		if err := v.Err(); err != nil {
			return "", fmt.Errorf("value %d does not compile: %w", i+2, err)
		}
		result = result.Unify(v)
	}
	if err := result.Err(); err != nil {
		return "", fmt.Errorf("the values conflict: %w", err)
	}

	node := result.Syntax(cue.Final())
	out, err := format.Node(node)
	if err != nil {
		return "", fmt.Errorf("cannot render the result: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Export resolves a CUE value to JSON, applying defaults.
func Export(source string) (string, error) {
	ctx := newContext()
	v := ctx.CompileString(source)
	if err := v.Err(); err != nil {
		return "", fmt.Errorf("does not compile: %w", err)
	}
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return "", fmt.Errorf("not fully specified: %w", err)
	}
	out, err := v.MarshalJSON()
	if err != nil {
		return "", fmt.Errorf("cannot export: %w", err)
	}
	return string(out), nil
}

// Format canonically formats CUE source, the way gofmt does for Go.
func Format(source string) (string, error) {
	ctx := newContext()
	v := ctx.CompileString(source)
	if err := v.Err(); err != nil {
		return "", fmt.Errorf("does not compile: %w", err)
	}
	out, err := format.Node(v.Syntax())
	if err != nil {
		return "", fmt.Errorf("cannot format: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
