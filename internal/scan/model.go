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
	Classes []Class
	Enums   []Enum
	Aliases []Alias

	// Whether these built-ins are referenced anywhere.
	UsesBytes       bool
	UsesISODateTime bool

	// Notes are things the scan decided not to expose, and why. Printed rather
	// than dropped, so it is always clear what is and is not covered.
	Notes []string
}

// Class is a Go type whose identity lives in Go: a named struct with exported
// methods, only ever seen behind a pointer.
//
// It does not cross the boundary as data. JavaScript holds an integer handle
// and every member is a call, so a method sees exactly the object the last one
// mutated. See internal/wasmbridge/runtime/handles.go.
type Class struct {
	Name    string
	Doc     string
	Methods []Func
	Fields  []Accessor

	// HasGoClose records a Go Close() error, which the generated close() calls
	// before releasing the handle -- so `await using` closes the resource
	// rather than merely forgetting about it.
	HasGoClose bool

	// Named is the Go type, kept so generators can walk it.
	Named *types.Named
}

// Accessor is one exported field of a class, reachable as a property getter
// and a setter rather than as data.
//
// It is a call rather than a stored property because the Go object is mutable:
// a value copied when the handle was created would go stale the moment a method
// touched it, with nothing in the type to say so.
type Accessor struct {
	GoName   string // Name
	JSName   string // name
	SetName  string // setName
	Doc      string
	Type     types.Type
	TS       string
	IsInt64  bool
	Optional bool
}

// Func is one exported Go function exposed to JavaScript.
type Func struct {
	GoName string // ExtractURLs
	JSName string // extractURLs
	Doc    string
	Pos    string

	// Recv names the class this is a method of, empty for a free function. It
	// qualifies the wire name so two types may both export Get.
	Recv string

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
	name := f.JSName
	if f.Recv != "" {
		name = f.Recv + "." + name
	}
	if m.Namespace == "" {
		return name
	}
	return m.Namespace + "." + name
}

// InvolvesClass reports whether a function's signature mentions a class.
//
// Such a call has no literal form: the recorder would marshal the Go value as
// data, while the generated TypeScript expects an object with an identity, so a
// recorded fixture would compare two unrelated things.
func (m *Module) InvolvesClass(f Func) bool {
	names := map[string]bool{}
	for _, c := range m.Classes {
		names[c.Name] = true
	}
	if len(names) == 0 {
		return false
	}
	mentions := func(t types.Type) bool { return mentionsNamed(t, names, map[types.Type]bool{}) }
	for _, p := range f.Params {
		if mentions(p.Type) {
			return true
		}
	}
	for _, r := range f.Results {
		if mentions(r.Type) {
			return true
		}
	}
	return false
}

func mentionsNamed(t types.Type, names map[string]bool, seen map[types.Type]bool) bool {
	if t == nil || seen[t] {
		return false
	}
	seen[t] = true
	switch u := types.Unalias(t).(type) {
	case *types.Pointer:
		return mentionsNamed(u.Elem(), names, seen)
	case *types.Slice:
		return mentionsNamed(u.Elem(), names, seen)
	case *types.Array:
		return mentionsNamed(u.Elem(), names, seen)
	case *types.Map:
		return mentionsNamed(u.Elem(), names, seen)
	case *types.Named:
		return names[u.Obj().Name()]
	}
	return false
}

// WireAccessor names a field's getter and setter on the wire.
func (m *Module) WireAccessor(c Class, a Accessor) (get, set string) {
	get = m.Wire(Func{Recv: c.Name, JSName: a.JSName})
	set = m.Wire(Func{Recv: c.Name, JSName: a.SetName})
	return get, set
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
