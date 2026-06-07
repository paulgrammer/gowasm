package scan

import (
	"strings"
	"testing"

	"github.com/paulgrammer/gowasm/internal/tsmap"
)

func load(t *testing.T) *Module {
	t.Helper()
	mod, err := Load(Options{Dir: "testdata/mapping", Pattern: ".", Int64: tsmap.Int64Number})
	if err != nil {
		t.Fatalf("loading testdata: %v", err)
	}
	return mod
}

func TestFunctionSignatures(t *testing.T) {
	mod := load(t)

	sigs := map[string]string{}
	for _, f := range mod.Funcs {
		params := ""
		for i, p := range f.Params {
			if i > 0 {
				params += ", "
			}
			params += p.JSName + ": " + p.TS
		}
		sigs[f.JSName] = "(" + params + ") => " + f.TSReturn()
	}

	want := map[string]string{
		"basics":    "(s: string, b: boolean, i: number, f: number) => string",
		"enums":     "(st: Status, p: Priority) => Status",
		"aliased":   "(u: Untagged) => Untagged",
		"nested":    "(o: Outer) => Outer",
		"bytes":     "(data: Uint8Array) => Uint8Array",
		"tuple":     "(s: string) => [number, string]",
		"nothing":   "() => void",
		"onlyError": "() => void",
		"variadic":  "(prefix: string, rest: number) => string",
		"pointers":  "(in: Inner | null) => Outer | null",
		"maps":      "(m: Record<string, Inner>) => Record<string, Uint8Array>",
		"anything":  "(v: unknown) => unknown",
	}
	for name, expect := range want {
		if got := sigs[name]; got != expect {
			t.Errorf("%s:\n  got  %s\n  want %s", name, got, expect)
		}
	}
}

func TestErrorAndVariadicFlags(t *testing.T) {
	mod := load(t)
	byName := map[string]Func{}
	for _, f := range mod.Funcs {
		byName[f.JSName] = f
	}

	if !byName["nested"].ReturnsError {
		t.Error("nested returns an error and should be marked as such")
	}
	if byName["basics"].ReturnsError {
		t.Error("basics returns no error")
	}
	if !byName["onlyError"].ReturnsError || len(byName["onlyError"].Results) != 0 {
		t.Error("a lone error result should produce no values and reject on failure")
	}
	if !byName["variadic"].Variadic {
		t.Error("variadic should be flagged")
	}
}

func TestEnumsBecomeLiteralUnions(t *testing.T) {
	mod := load(t)
	enums := map[string][]string{}
	for _, e := range mod.Enums {
		for _, m := range e.Members {
			enums[e.Name] = append(enums[e.Name], m.Literal)
		}
	}

	if got := enums["Status"]; len(got) != 2 || got[0] != `"active"` || got[1] != `"archived"` {
		t.Errorf("Status members = %v, want the two string literals", got)
	}
	if got := enums["Priority"]; len(got) != 2 || got[0] != "9" || got[1] != "1" {
		t.Errorf("Priority members = %v, want the two int literals", got)
	}

	// A named scalar with no constants keeps its identity as an alias.
	var found bool
	for _, a := range mod.Aliases {
		if a.Name == "Untagged" && a.TS == "string" {
			found = true
		}
	}
	if !found {
		t.Error("Untagged should be emitted as an alias to string")
	}
}

func TestStructFields(t *testing.T) {
	mod := load(t)
	var outer Struct
	for _, s := range mod.Structs {
		if s.Name == "Outer" {
			outer = s
		}
	}
	if outer.Name == "" {
		t.Fatal("Outer was not collected")
	}

	if len(outer.Extends) != 1 || outer.Extends[0] != "Inner" {
		t.Errorf("Extends = %v, want [Inner] for the embedded struct", outer.Extends)
	}

	fields := map[string]Field{}
	for _, f := range outer.Fields {
		fields[f.JSName] = f
	}

	cases := []struct {
		name, ts string
		optional bool
	}{
		// No json tag: encoding/json emits the Go field name verbatim, so
		// camelCasing here would make the declaration disagree with the wire.
		{"Name", "string", false},
		{"renamed", "string", false},
		{"optional", "string", true},
		{"ptr", "Inner | null", false},
		{"blob", "Uint8Array", false},
		{"when", "ISODateTime", false},
		{"big", "number", false},
		{"anything", "unknown", false},
		{"counts", "Record<string, number>", false},
		{"children", "Inner[]", false},
		{"selfRef", "Outer | null", false},
	}
	for _, c := range cases {
		f, ok := fields[c.name]
		if !ok {
			t.Errorf("field %q missing", c.name)
			continue
		}
		if f.TS != c.ts {
			t.Errorf("field %q type = %q, want %q", c.name, f.TS, c.ts)
		}
		if f.Optional != c.optional {
			t.Errorf("field %q optional = %v, want %v", c.name, f.Optional, c.optional)
		}
	}

	for _, skipped := range []string{"Ignored", "-", "unexported"} {
		if _, present := fields[skipped]; present {
			t.Errorf("field %q should not be emitted", skipped)
		}
	}
	if !fields["big"].IsInt64 {
		t.Error("an int64 field should be flagged so a precision note can be emitted")
	}
}

func TestBuiltinsOnlyWhenUsed(t *testing.T) {
	mod := load(t)
	if !mod.UsesBytes {
		t.Error("UsesBytes should be set: the package has []byte")
	}
	if !mod.UsesISODateTime {
		t.Error("UsesISODateTime should be set: the package has time.Time")
	}
}

// A lone package keeps the flat API, so its wire names and its generated
// TypeScript are exactly what they were before namespacing existed.
func TestOnePackageIsNotNamespaced(t *testing.T) {
	b, err := LoadAll(Options{Dir: "testdata/mapping", Int64: tsmap.Int64Number},
		[]Package{{Pattern: ".", Namespace: "mapping"}})
	if err != nil {
		t.Fatal(err)
	}
	if b.Multi() {
		t.Error("one package must not be treated as several")
	}
	mod := b.Modules[0]
	if mod.Namespace != "" {
		t.Errorf("namespace = %q, want empty for a lone package", mod.Namespace)
	}
	if got := mod.Wire(mod.Funcs[0]); got != mod.Funcs[0].JSName {
		t.Errorf("wire name = %q, want the bare %q", got, mod.Funcs[0].JSName)
	}
}

// Two packages that both export the same name are the whole point: the
// namespace is what keeps them apart on the wire.
func TestSeveralPackagesAreNamespaced(t *testing.T) {
	b, err := LoadAll(Options{Dir: "testdata/mapping", Int64: tsmap.Int64Number},
		[]Package{{Pattern: ".", Namespace: "a"}, {Pattern: ".", Namespace: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if !b.Multi() {
		t.Fatal("two packages must namespace their exports")
	}
	first := b.Modules[0].Funcs[0]
	wireA := b.Modules[0].Wire(first)
	wireB := b.Modules[1].Wire(first)
	if wireA == wireB {
		t.Errorf("both packages register %q; the namespace is not being applied", wireA)
	}
	if wireA != "a."+first.JSName {
		t.Errorf("wire name = %q, want %q", wireA, "a."+first.JSName)
	}
}

// A missing package has to name the path, not just fail somewhere inside the
// loader with no clue which entry was wrong.
func TestLoadAllNamesTheOffendingPackage(t *testing.T) {
	_, err := LoadAll(Options{Dir: "testdata/mapping", Int64: tsmap.Int64Number},
		[]Package{{Pattern: ".", Namespace: "a"}, {Pattern: "./nope", Namespace: "nope"}})
	if err == nil {
		t.Fatal("a package that does not exist should fail")
	}
	if !strings.Contains(err.Error(), "./nope") {
		t.Errorf("the error should name ./nope, got: %v", err)
	}
}
