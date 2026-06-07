// Package scan reads a Go package and produces the intermediate representation
// every generator consumes.
package scan

import "go/types"

// Bundle is every package a project exposes, in the order they were declared.
//
// One package is the common case and keeps the flat API: a Bundle of one
// generates exactly what a single package always generated.
type Bundle struct {
	Modules []*Module
}

// Multi reports whether exports are namespaced by package.
func (b *Bundle) Multi() bool { return len(b.Modules) > 1 }

// Module is everything the generators need about one Go package.
type Module struct {
	// Namespace is the TypeScript name this package's exports live under, and
	// the prefix its registrations carry across the boundary. It is empty for
	// the flat, single-package case.
	Namespace string

	PkgPath string
	PkgName string
	Funcs   []Func
	Structs []Struct
	Enums   []Enum
	Aliases []Alias

	// Whether these built-ins are referenced anywhere.
	UsesBytes       bool
	UsesISODateTime bool
}

// Func is one exported Go function exposed to JavaScript.
type Func struct {
	GoName string // ExtractURLs
	JSName string // extractURLs
	Doc    string
	Pos    string

	// HasContext records a leading context.Context parameter, which is stripped
	// from the JS signature; the bridge passes context.Background().
	HasContext bool

	Params   []Param
	Results  []Result // non-error results only
	Variadic bool

	// ReturnsError records a trailing error result, which becomes a rejected
	// promise rather than a value.
	ReturnsError bool
}

// Param is one JS-visible parameter.
type Param struct {
	GoName  string
	JSName  string
	Type    types.Type
	TS      string
	IsInt64 bool
}

// Result is one non-error return value.
type Result struct {
	Type    types.Type
	TS      string
	IsInt64 bool
}

// Struct becomes a TypeScript interface.
type Struct struct {
	Name    string
	Doc     string
	Extends []string
	Fields  []Field

	// Named is the Go type, kept so generators can walk it, for example to
	// find the []byte fields that need converting to a typed array.
	Named *types.Named
}

// Field is one JSON-visible struct field.
type Field struct {
	JSName   string
	TS       string
	Optional bool
	Doc      string
	IsInt64  bool
	Type     types.Type
}

// Enum is a named scalar with declared constants, emitted as a literal union.
type Enum struct {
	Name    string
	Doc     string
	Members []EnumMember
}

// EnumMember carries the constant's TypeScript literal form, already quoted for
// string kinds.
type EnumMember struct {
	GoName  string
	Literal string
}

// Alias is a named type with no constants, emitted as a TypeScript alias so the
// name still appears in signatures.
type Alias struct {
	Name string
	Doc  string
	TS   string
}

// Wire is the name a function is registered under on the JavaScript side.
// Namespacing keeps two packages that both export New from colliding.
func (m *Module) Wire(f Func) string {
	if m.Namespace == "" {
		return f.JSName
	}
	return m.Namespace + "." + f.JSName
}

// TSReturn renders the promise payload for a function: void, a single type, or
// a tuple when Go returns several non-error values.
func (f Func) TSReturn() string {
	switch len(f.Results) {
	case 0:
		return "void"
	case 1:
		return f.Results[0].TS
	default:
		out := "["
		for i, r := range f.Results {
			if i > 0 {
				out += ", "
			}
			out += r.TS
		}
		return out + "]"
	}
}
