// Package gobridge emits the generated main package: the registrations for
// every exported function, plus the runtime that serves them.
//
// The output lives in its own package (.gowasm/wasmmain) rather than inside the
// package being scanned. That keeps the user's package always type-checkable:
// there is never a state where generating requires code that generation itself
// produces. Neither file is written to disk; both reach the compiler through an
// overlay.
package gobridge

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/format"
	"go/types"
	"sort"
	"strings"
	"text/template"

	"github.com/paulgrammer/gowasm/internal/scan"
)

//go:embed templates/main.go.tmpl
var mainTmpl string

type importSpec struct {
	Alias string
	Path  string
}

type fn struct {
	GoName string
	JSName string
	// Wire is the name the function is registered under: namespaced by package
	// when the project exposes more than one.
	Wire    string
	Doc     string
	Arity   int
	Decodes []string
	Body    []string
}

type data struct {
	Namespace string
	Imports   []importSpec
	Funcs     []fn
}

// classSet identifies the named types that cross as handles rather than as
// JSON, so a result can be retained and a parameter resolved.
type classSet map[*types.TypeName]bool

func (cs classSet) has(t types.Type) (*types.Named, bool) {
	p, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return nil, false
	}
	n, ok := types.Unalias(p.Elem()).(*types.Named)
	if !ok || !cs[n.Obj()] {
		return nil, false
	}
	return n, true
}

// Generate renders the bridge source for every package in the bundle.
//
// One wasm module serves them all: splitting per package would multiply a
// multi-megabyte module by the number of packages, and the Go runtime inside it
// is the bulk of that weight either way.
func Generate(b *scan.Bundle, namespace string) ([]byte, error) {
	imp := newImports()
	d := data{Namespace: namespace}

	// Every exposed package is registered before anything is rendered: one of
	// them may return a type declared in another, and the qualifier has to know
	// the alias by the time it sees that type rather than after.
	aliases := make([]string, len(b.Modules))
	for i, mod := range b.Modules {
		aliases[i] = imp.user(mod)
	}

	// Handles are per-instance and type-agnostic, so one set spanning every
	// package in the bundle is exactly right.
	classes := classSet{}
	for _, mod := range b.Modules {
		for _, c := range mod.Classes {
			if c.Named != nil {
				classes[c.Named.Obj()] = true
			}
		}
	}

	for i, mod := range b.Modules {
		alias := aliases[i]
		for _, f := range mod.Funcs {
			g, err := renderFunc(f, mod.Wire(f), alias, "", imp, classes)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", f.Pos, f.GoName, err)
			}
			d.Funcs = append(d.Funcs, g)
		}
		for _, c := range mod.Classes {
			recv := types.TypeString(types.NewPointer(c.Named), imp.qualifier)
			for _, mth := range c.Methods {
				g, err := renderFunc(mth, mod.Wire(mth), alias, recv, imp, classes)
				if err != nil {
					return nil, fmt.Errorf("%s: %s.%s: %w", mth.Pos, c.Name, mth.GoName, err)
				}
				d.Funcs = append(d.Funcs, g)
			}
			if c.HasGoClose {
				d.Funcs = append(d.Funcs, renderGoClose(mod, c, recv))
			}
			for _, a := range c.Fields {
				get, set := mod.WireAccessor(c, a)
				d.Funcs = append(d.Funcs, renderGetter(c, a, get, recv, imp))
				d.Funcs = append(d.Funcs, renderSetter(c, a, set, recv, imp))
			}
		}
	}
	d.Imports = imp.specs()

	t, err := template.New("main").Parse(mainTmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return nil, err
	}

	src, err := format.Source(buf.Bytes())
	if err != nil {
		// Emitting unformatted source would hide the real error behind a
		// compile failure later, so surface it with the offending output.
		return nil, fmt.Errorf("generated bridge is not valid Go: %w\n%s", err, buf.String())
	}
	return src, nil
}

// renderFunc emits one registration. recvType is empty for a free function; for
// a method it is the pointer type of the receiver, which arrives as a handle in
// the first argument.
func renderFunc(f scan.Func, wire, alias, recvType string, imp *imports, classes classSet) (fn, error) {
	g := fn{
		GoName: f.GoName,
		JSName: f.JSName,
		Wire:   wire,
		Doc:    firstLine(f.Doc),
		Arity:  len(f.Params),
	}

	// The receiver rides in argument 0, so a method's wire arity is one more
	// than the signature suggests.
	shift := 0
	if recvType != "" {
		g.Arity++
		shift = 1
		g.Decodes = append(g.Decodes, borrowLines("self", 0, recvType, wire)...)
	}

	args := make([]string, 0, len(f.Params)+1)
	if f.HasContext {
		imp.add("context")
		args = append(args, "context.Background()")
	}

	for i, p := range f.Params {
		at := i + shift
		where := fmt.Sprintf("%s: argument %d (%s)", f.JSName, i+1, p.GoName)

		if n, isClass := classes.has(p.Type); isClass {
			goType := types.TypeString(types.NewPointer(n), imp.qualifier)
			g.Decodes = append(g.Decodes, borrowOptional(fmt.Sprintf("p%d", i), at, goType, where)...)
		} else {
			goType := types.TypeString(p.Type, imp.qualifier)
			g.Decodes = append(g.Decodes,
				fmt.Sprintf("var p%d %s", i, goType),
				fmt.Sprintf("if err := json.Unmarshal(a[%d], &p%d); err != nil {", at, i),
				fmt.Sprintf("\treturn nil, fmt.Errorf(%q, err)", where+": %w"),
				"}",
			)
		}
		arg := fmt.Sprintf("p%d", i)
		if f.Variadic && i == len(f.Params)-1 {
			arg += "..."
		}
		args = append(args, arg)
	}

	// A handle can only be a whole result. Emitting one inside a tuple would
	// put a bare integer in a slot the TypeScript side has typed as a class,
	// with nothing to catch it.
	if len(f.Results) > 1 {
		for _, r := range f.Results {
			if n, isClass := classes.has(r.Type); isClass {
				return g, fmt.Errorf("returns %s alongside other values; a class must be the only result", n.Obj().Name())
			}
		}
	}

	target := alias
	if recvType != "" {
		target = "self"
	}
	call := fmt.Sprintf("%s.%s(%s)", target, f.GoName, strings.Join(args, ", "))

	// A single class result is retained and travels as its handle.
	if len(f.Results) == 1 {
		if _, isClass := classes.has(f.Results[0].Type); isClass {
			if f.ReturnsError {
				g.Body = append(g.Body,
					fmt.Sprintf("r0, err := %s", call),
					"if err != nil {",
					"\treturn nil, err",
					"}",
				)
			} else {
				g.Body = append(g.Body, fmt.Sprintf("r0 := %s", call))
			}
			// The nil check lives here, where the static type is known: a nil
			// *T inside an `any` is not itself nil, so Retain could not see it.
			g.Body = append(g.Body,
				"if r0 == nil {",
				"\treturn int64(0), nil",
				"}",
				"return Retain(r0), nil",
			)
			return g, nil
		}
	}

	nres := len(f.Results)
	switch {
	case nres == 0 && !f.ReturnsError:
		g.Body = append(g.Body, call, "return nil, nil")

	case nres == 0 && f.ReturnsError:
		g.Body = append(g.Body,
			fmt.Sprintf("if err := %s; err != nil {", call),
			"\treturn nil, err",
			"}",
			"return nil, nil",
		)

	case nres == 1 && !f.ReturnsError:
		g.Body = append(g.Body, fmt.Sprintf("return %s, nil", call))

	case nres == 1 && f.ReturnsError:
		g.Body = append(g.Body,
			fmt.Sprintf("r0, err := %s", call),
			"if err != nil {",
			"\treturn nil, err",
			"}",
			"return r0, nil",
		)

	default:
		lhs := make([]string, 0, nres+1)
		for i := 0; i < nres; i++ {
			lhs = append(lhs, fmt.Sprintf("r%d", i))
		}
		vals := strings.Join(lhs, ", ")
		if f.ReturnsError {
			g.Body = append(g.Body,
				fmt.Sprintf("%s, err := %s", vals, call),
				"if err != nil {",
				"\treturn nil, err",
				"}",
			)
		} else {
			g.Body = append(g.Body, fmt.Sprintf("%s := %s", vals, call))
		}
		// Multiple results arrive in JS as a tuple, which JSON encodes as an array.
		g.Body = append(g.Body, fmt.Sprintf("return []any{%s}, nil", vals))
	}

	return g, nil
}

// borrowLines resolves a handle that must be present, for a method receiver.
func borrowLines(name string, at int, goType, wire string) []string {
	return []string{
		fmt.Sprintf("var %sHandle int64", name),
		fmt.Sprintf("if err := json.Unmarshal(a[%d], &%sHandle); err != nil {", at, name),
		fmt.Sprintf("\treturn nil, fmt.Errorf(%q, err)", wire+": receiver: %w"),
		"}",
		fmt.Sprintf("%sValue, %sDone, err := Borrow(%sHandle)", name, name, name),
		"if err != nil {",
		fmt.Sprintf("\treturn nil, fmt.Errorf(%q, err)", wire+": %w"),
		"}",
		fmt.Sprintf("defer %sDone()", name),
		fmt.Sprintf("%s, ok := %sValue.(%s)", name, name, goType),
		"if !ok {",
		fmt.Sprintf("\treturn nil, fmt.Errorf(%q, %sHandle)", wire+": handle %d does not hold a "+strings.TrimPrefix(goType, "*"), name),
		"}",
	}
}

// borrowOptional resolves a handle argument that may be null, which arrives as
// handle 0.
func borrowOptional(name string, at int, goType, where string) []string {
	return []string{
		fmt.Sprintf("var %sHandle int64", name),
		fmt.Sprintf("if err := json.Unmarshal(a[%d], &%sHandle); err != nil {", at, name),
		fmt.Sprintf("\treturn nil, fmt.Errorf(%q, err)", where+": %w"),
		"}",
		fmt.Sprintf("var %s %s", name, goType),
		fmt.Sprintf("if %sHandle != 0 {", name),
		fmt.Sprintf("\tvalue, done, err := Borrow(%sHandle)", name),
		"\tif err != nil {",
		fmt.Sprintf("\t\treturn nil, fmt.Errorf(%q, err)", where+": %w"),
		"\t}",
		"\tdefer done()",
		"\tvar ok bool",
		fmt.Sprintf("\t%s, ok = value.(%s)", name, goType),
		"\tif !ok {",
		fmt.Sprintf("\t\treturn nil, fmt.Errorf(%q, %sHandle)", where+": handle %d does not hold a "+strings.TrimPrefix(goType, "*"), name),
		"\t}",
		"}",
	}
}

// renderGoClose emits the registration behind the generated close(), for a type
// that declares its own Close() error. Releasing the handle is the JS side's
// job; this is only the user's cleanup.
func renderGoClose(mod *scan.Module, c scan.Class, recvType string) fn {
	wire := mod.Wire(scan.Func{Recv: c.Name, JSName: "__goClose"})
	return fn{
		GoName:  "Close",
		JSName:  "__goClose",
		Wire:    wire,
		Arity:   1,
		Decodes: borrowLines("self", 0, recvType, wire),
		Body: []string{
			"if err := self.Close(); err != nil {",
			"\treturn nil, err",
			"}",
			"return nil, nil",
		},
	}
}

// renderGetter reads one exported field. It is a call rather than part of the
// handle because the object is mutable: a value copied when the handle was made
// would be stale the moment a method touched it.
func renderGetter(c scan.Class, a scan.Accessor, wire, recvType string, imp *imports) fn {
	return fn{
		GoName:  a.GoName,
		JSName:  a.JSName,
		Wire:    wire,
		Doc:     firstLine(a.Doc),
		Arity:   1,
		Decodes: borrowLines("self", 0, recvType, wire),
		Body:    []string{fmt.Sprintf("return self.%s, nil", a.GoName)},
	}
}

// renderSetter writes one exported field.
func renderSetter(c scan.Class, a scan.Accessor, wire, recvType string, imp *imports) fn {
	goType := types.TypeString(a.Type, imp.qualifier)
	decodes := borrowLines("self", 0, recvType, wire)
	decodes = append(decodes,
		fmt.Sprintf("var v %s", goType),
		"if err := json.Unmarshal(a[1], &v); err != nil {",
		fmt.Sprintf("\treturn nil, fmt.Errorf(%q, err)", wire+": %w"),
		"}",
	)
	return fn{
		GoName:  a.GoName,
		JSName:  a.SetName,
		Wire:    wire,
		Arity:   2,
		Decodes: decodes,
		Body: []string{
			fmt.Sprintf("self.%s = v", a.GoName),
			"return nil, nil",
		},
	}
}

// --- import bookkeeping ---

type imports struct {
	// userAliases maps each exposed package path to the alias it is imported
	// under, so a type in one package and a type in another never collide.
	userAliases map[string]string
	byPath      map[string]string // path -> alias ("" means use the package name)
}

func newImports() *imports {
	i := &imports{userAliases: map[string]string{}, byPath: map[string]string{}}
	// Always needed by the emitted code.
	i.add("encoding/json")
	i.add("fmt")
	i.add("os")
	return i
}

// user registers an exposed package and returns its alias.
//
// A lone package keeps the name userpkg it has always had, so single-package
// output is byte-for-byte what it was.
func (i *imports) user(mod *scan.Module) string {
	alias := "userpkg"
	if mod.Namespace != "" {
		alias = "pkg_" + mod.Namespace
	}
	i.userAliases[mod.PkgPath] = alias
	i.addAlias(alias, mod.PkgPath)
	return alias
}

func (i *imports) add(path string)             { i.addAlias("", path) }
func (i *imports) addAlias(alias, path string) { i.byPath[path] = alias }

// qualifier names a package inside generated source, registering the import as
// a side effect so the import block always matches what the body references.
func (i *imports) qualifier(p *types.Package) string {
	if p == nil {
		return ""
	}
	if alias, exposed := i.userAliases[p.Path()]; exposed {
		i.addAlias(alias, p.Path())
		return alias
	}
	i.add(p.Path())
	return p.Name()
}

func (i *imports) specs() []importSpec {
	paths := make([]string, 0, len(i.byPath))
	for p := range i.byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	out := make([]importSpec, 0, len(paths))
	for _, p := range paths {
		out = append(out, importSpec{Alias: i.byPath[p], Path: p})
	}
	return out
}

func firstLine(s string) string {
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
