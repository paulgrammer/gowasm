// Package expr evaluates user-supplied expressions safely.
//
// The JavaScript answer to "let users write a rule" is usually eval, or new
// Function, which hands the author of the string everything the page can do.
// The safe alternatives mean shipping an interpreter and hoping the sandbox
// holds.
//
// expr-lang is a small expression language with no I/O, no loops that can run
// forever, and no access to anything the host does not pass in. An expression
// is compiled once and can then be run against many inputs, and compilation
// reports type errors before anything is evaluated.
//
// This is the shape of thing that turns up in feature flags, alerting rules,
// pricing logic and access policies: user-authored, frequently evaluated, and
// absolutely not something you want to eval.
package expr

import (
	"fmt"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// Value is the result of an evaluation, tagged with what it turned out to be.
type Value struct {
	// Kind is bool, number, string, array, object or null.
	Kind string `json:"kind"`
	// JSON is the value itself.
	JSON any `json:"json"`
}

// Timing reports how long an expression takes, separated from Value so that a
// result stays comparable between runs.
type Timing struct {
	Runs         int   `json:"runs"`
	Microseconds int64 `json:"microseconds"`
}

// Program is a compiled expression.
type Program struct {
	Source string `json:"source"`
	// Disassembly is the bytecode, which is what makes it obvious that this is
	// not eval: there is a fixed instruction set and no way out of it.
	Disassembly string `json:"disassembly"`
	// Constants counts the literals the compiler folded.
	Constants int `json:"constants"`
}

func kindOf(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func compile(source string, env map[string]any) (*vm.Program, error) {
	opts := []expr.Option{expr.Env(env)}
	p, err := expr.Compile(source, opts...)
	if err != nil {
		return nil, fmt.Errorf("cannot compile %q: %w", source, err)
	}
	return p, nil
}

// Compile checks an expression against the shape of an environment without
// running it, so a bad rule is rejected at the point it is written.
func Compile(source string, env map[string]any) (Program, error) {
	p, err := compile(source, env)
	if err != nil {
		return Program{}, err
	}
	return Program{
		Source:      source,
		Disassembly: p.Disassemble(),
		Constants:   len(p.Constants),
	}, nil
}

// Eval compiles and runs an expression against an environment.
func Eval(source string, env map[string]any) (Value, error) {
	p, err := compile(source, env)
	if err != nil {
		return Value{}, err
	}

	out, err := expr.Run(p, env)
	if err != nil {
		return Value{}, fmt.Errorf("evaluating %q: %w", source, err)
	}
	return Value{Kind: kindOf(out), JSON: out}, nil
}

// Benchmark compiles once and evaluates many times, which is the point of
// compiling at all.
func Benchmark(source string, env map[string]any, runs int) (Timing, error) {
	if runs < 1 || runs > 1000000 {
		return Timing{}, fmt.Errorf("runs must be between 1 and 1000000, got %d", runs)
	}
	p, err := compile(source, env)
	if err != nil {
		return Timing{}, err
	}

	start := time.Now()
	for range runs {
		if _, err := expr.Run(p, env); err != nil {
			return Timing{}, fmt.Errorf("evaluating %q: %w", source, err)
		}
	}
	return Timing{Runs: runs, Microseconds: time.Since(start).Microseconds()}, nil
}

// Test evaluates an expression that must produce a boolean, which is the usual
// case for a rule.
func Test(source string, env map[string]any) (bool, error) {
	v, err := Eval(source, env)
	if err != nil {
		return false, err
	}
	b, ok := v.JSON.(bool)
	if !ok {
		return false, fmt.Errorf("%q produced a %s, but a rule must produce a bool", source, v.Kind)
	}
	return b, nil
}

// EvalAll runs one expression against many environments, which is what a rules
// engine actually does: compile once, evaluate per record.
func EvalAll(source string, envs []map[string]any) ([]Value, error) {
	if len(envs) == 0 {
		return []Value{}, nil
	}
	p, err := compile(source, envs[0])
	if err != nil {
		return nil, err
	}

	out := make([]Value, 0, len(envs))
	for i, env := range envs {
		v, err := expr.Run(p, env)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", i, err)
		}
		out = append(out, Value{Kind: kindOf(v), JSON: v})
	}
	return out, nil
}
