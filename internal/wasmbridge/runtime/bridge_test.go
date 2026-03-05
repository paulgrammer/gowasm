//go:build js && wasm

package runtime

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Nil slices and maps must reach JavaScript as empty collections. The generated
// TypeScript declares T[] and Record<...>, so a JSON null would be a type the
// caller was promised could not occur.
func TestNormalizeNils(t *testing.T) {
	type inner struct {
		Tags []string `json:"tags"`
	}
	type outer struct {
		Names   []string         `json:"names"`
		Counts  map[string]int   `json:"counts"`
		Nested  inner            `json:"nested"`
		Ptr     *inner           `json:"ptr"`
		List    []inner          `json:"list"`
		ByKey   map[string]inner `json:"byKey"`
		Blob    []byte           `json:"blob"`
		Present []string         `json:"present"`
	}

	in := outer{
		Ptr:     &inner{},
		List:    []inner{{}},
		ByKey:   map[string]inner{"a": {}},
		Present: []string{"kept"},
	}

	got, err := json.Marshal(normalized(in))
	if err != nil {
		t.Fatal(err)
	}

	want := `{"names":[],"counts":{},"nested":{"tags":[]},"ptr":{"tags":[]},` +
		`"list":[{"tags":[]}],"byKey":{"a":{"tags":[]}},"blob":"","present":["kept"]}`
	if string(got) != want {
		t.Errorf("normalized:\n got  %s\n want %s", got, want)
	}
}

func TestNormalizeNilsLeavesScalarsAlone(t *testing.T) {
	for _, v := range []any{nil, 42, "text", true, 1.5} {
		got := normalized(v)
		if v == nil {
			if got != nil {
				t.Errorf("nil should stay nil, got %v", got)
			}
			continue
		}
		if !reflect.DeepEqual(got, v) {
			t.Errorf("normalized(%v) = %v, want it unchanged", v, got)
		}
	}
}

func TestNormalizeTopLevelNilSlice(t *testing.T) {
	var nilSlice []string
	out, err := json.Marshal(normalized(nilSlice))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "[]" {
		t.Errorf("a nil slice result = %s, want []", out)
	}

	var nilMap map[string]int
	out, err = json.Marshal(normalized(nilMap))
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "{}" {
		t.Errorf("a nil map result = %s, want {}", out)
	}
}

// Arity is checked before the handler runs, so a caller cannot silently get
// zero values for arguments they forgot.
func TestInvokeChecksArity(t *testing.T) {
	e := &entry{
		name:  "example",
		arity: 2,
		fn:    func([]json.RawMessage) (any, error) { return "ok", nil },
	}
	if _, err := e.invoke(`["only one"]`); err == nil {
		t.Error("a call with too few arguments should fail")
	}
	if _, err := e.invoke(`["a","b","c"]`); err == nil {
		t.Error("a call with too many arguments should fail")
	}
	if _, err := e.invoke(`not json`); err == nil {
		t.Error("a malformed argument list should fail")
	}
	out, err := e.invoke(`["a","b"]`)
	if err != nil {
		t.Fatal(err)
	}
	if out != `"ok"` {
		t.Errorf("result = %s, want the marshalled value", out)
	}
}

// A panic must fail one call, not tear down the whole instance: every other
// exported function would die with it and there is no way to restart.
func TestInvokeRecoversFromPanic(t *testing.T) {
	e := &entry{
		name:  "boom",
		arity: 0,
		fn:    func([]json.RawMessage) (any, error) { panic("handler exploded") },
	}
	_, err := e.invoke(`[]`)
	if err == nil {
		t.Fatal("a panicking handler should produce an error")
	}
	if got := err.Error(); got != "boom: panic: handler exploded" {
		t.Errorf("error = %q, want it to name the function and the panic", got)
	}
}

// A function with no results resolves with the empty string, which the client
// maps to undefined.
func TestInvokeVoid(t *testing.T) {
	e := &entry{
		name:  "ping",
		arity: 0,
		fn:    func([]json.RawMessage) (any, error) { return nil, nil },
	}
	out, err := e.invoke(`[]`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("a void result = %q, want the empty string", out)
	}
}
