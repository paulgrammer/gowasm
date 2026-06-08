package scan

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/paulgrammer/gowasm/internal/tsmap"
)

// Reserved JS names the bridge installs itself.
var reserved = map[string]string{
	"dispose": "the runtime installs dispose() for teardown",
}

// Options configures a scan.
type Options struct {
	Dir     string // directory to resolve Pattern from
	Pattern string // package pattern, e.g. "./urls"
	Int64   tsmap.Int64Mode
}

// Package names one package to scan, and the namespace its exports take.
type Package struct {
	Pattern   string
	Namespace string
}

// LoadAll type-checks every declared package.
//
// Each is loaded by the existing single-package path and keeps its own type
// mapper, so names stay per-package: two packages may both export a Config
// without either having to be renamed.
func LoadAll(opts Options, pkgs []Package) (*Bundle, error) {
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages to scan")
	}
	b := &Bundle{}
	for _, p := range pkgs {
		o := opts
		o.Pattern = p.Pattern
		mod, err := Load(o)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Pattern, err)
		}
		// A lone package keeps the flat API, so it carries no namespace.
		if len(pkgs) > 1 {
			mod.Namespace = p.Namespace
		}
		b.Modules = append(b.Modules, mod)
	}
	return b, nil
}

// Load type-checks the package and builds the IR.
func Load(opts Options) (*Module, error) {
	pkg, err := loadPackage(opts.Dir, opts.Pattern, false)
	if err != nil {
		return nil, err
	}

	docs := buildDocs(pkg)

	// Which types become classes is decided first, from the exported surface
	// alone. It cannot be worked out later: collectTypes discovers types while
	// it drains, so the answer would depend on the order it happened to reach
	// them. See resource.go.
	classes, err := classify(pkg, opts.Dir)
	if err != nil {
		return nil, err
	}

	m := tsmap.New(opts.Int64)

	mod := &Module{PkgPath: pkg.PkgPath, PkgName: pkg.Name}
	for _, name := range classes.demoted {
		mod.Notes = append(mod.Notes,
			fmt.Sprintf("%s has methods but is used as a value, so it stays a plain interface and its methods are not exposed", name))
	}

	scope := pkg.Types.Scope()
	names := scope.Names()
	sort.Strings(names)

	seen := map[string]string{} // JS name -> Go name, for collision reporting

	for _, name := range names {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		fn, ok := obj.(*types.Func)
		if !ok {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Recv() != nil {
			continue
		}

		f, err := buildFunc(pkg, opts.Dir, fn, sig, m, docs)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", pos(pkg, opts.Dir, fn.Pos()), fn.Name(), err)
		}

		if why, bad := reserved[f.JSName]; bad {
			return nil, fmt.Errorf("%s: %s maps to the reserved name %q (%s); rename the Go function",
				pos(pkg, opts.Dir, fn.Pos()), fn.Name(), f.JSName, why)
		}
		if prev, dup := seen[f.JSName]; dup {
			return nil, fmt.Errorf("%s: %s and %s both map to %q; rename one",
				pos(pkg, opts.Dir, fn.Pos()), prev, fn.Name(), f.JSName)
		}
		seen[f.JSName] = fn.Name()

		mod.Funcs = append(mod.Funcs, f)
	}

	// Classes are built before types are drained, so the parameters and results
	// of their methods reach the mapper in time to be discovered like any
	// others.
	classNames := make([]string, 0, len(classes.classes))
	byName := map[string]*types.Named{}
	for obj, named := range classes.classes {
		classNames = append(classNames, obj.Name())
		byName[obj.Name()] = named
	}
	sort.Strings(classNames)
	for _, name := range classNames {
		c, err := buildClass(pkg, opts.Dir, byName[name], m, docs, classes, mod)
		if err != nil {
			return nil, err
		}
		mod.Classes = append(mod.Classes, c)
	}

	if len(mod.Funcs) == 0 && len(mod.Classes) == 0 {
		return nil, fmt.Errorf("package %s exports no functions that can cross the boundary", pkg.PkgPath)
	}

	if err := collectTypes(pkg, opts.Dir, m, mod, docs, classes); err != nil {
		return nil, err
	}

	mod.UsesBytes = m.UsesBytes()
	mod.UsesISODateTime = m.UsesISODateTime()
	return mod, nil
}

func loadPackage(dir, pattern string, tests bool) (*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedSyntax | packages.NeedDeps | packages.NeedImports | packages.NeedFiles,
		Dir:   dir,
		Tests: tests,
		// Type-check for the target the code will actually be compiled for, so
		// build-tagged files resolve the same way `go build` will.
		Env: testEnv(),
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", pattern, err)
	}
	var chosen *packages.Package
	for _, p := range pkgs {
		// With Tests:true the loader returns several variants of one package;
		// the plain one has no bracket suffix in its ID.
		if strings.Contains(p.ID, "[") || strings.HasSuffix(p.PkgPath, ".test") {
			continue
		}
		chosen = p
		break
	}
	if chosen == nil && len(pkgs) > 0 {
		chosen = pkgs[0]
	}
	if chosen == nil {
		return nil, fmt.Errorf("no package matched %q in %s", pattern, dir)
	}
	if len(chosen.Errors) > 0 {
		var b strings.Builder
		for _, e := range chosen.Errors {
			fmt.Fprintf(&b, "\n  %s", e)
		}
		return nil, fmt.Errorf("package %s has errors:%s", chosen.PkgPath, b.String())
	}
	if chosen.Types == nil {
		return nil, fmt.Errorf("package %s could not be type-checked", pattern)
	}
	return chosen, nil
}

func buildFunc(pkg *packages.Package, base string, fn *types.Func, sig *types.Signature, m *tsmap.Mapper, docs docIndex) (Func, error) {
	f := Func{
		GoName:   fn.Name(),
		JSName:   tsmap.CamelCase(fn.Name()),
		Doc:      docs[fn.Pos()],
		Pos:      pos(pkg, base, fn.Pos()),
		Variadic: sig.Variadic(),
	}

	if sig.TypeParams() != nil && sig.TypeParams().Len() > 0 {
		return f, fmt.Errorf("generic functions are not supported")
	}

	params := sig.Params()
	start := 0
	if params.Len() > 0 && isContext(params.At(0).Type()) {
		f.HasContext = true
		start = 1
	}
	for i := start; i < params.Len(); i++ {
		p := params.At(i)
		if isContext(p.Type()) {
			return f, fmt.Errorf("context.Context must be the first parameter")
		}

		t := p.Type()
		if f.Variadic && i == params.Len()-1 {
			// The variadic parameter is declared as a slice; the TS signature
			// wants the element type behind a rest token.
			s, ok := types.Unalias(t).(*types.Slice)
			if !ok {
				return f, fmt.Errorf("variadic parameter is not a slice")
			}
			t = s.Elem()
		}

		ts, err := m.TS(t)
		if err != nil {
			return f, fmt.Errorf("parameter %s: %w", paramName(p, i), err)
		}
		f.Params = append(f.Params, Param{
			GoName:  paramName(p, i),
			JSName:  jsParamName(p, i),
			Type:    p.Type(),
			TS:      ts,
			IsInt64: tsmap.IsInt64(t),
		})
	}

	results := sig.Results()
	n := results.Len()
	if n > 0 && isError(results.At(n-1).Type()) {
		f.ReturnsError = true
		n--
	}
	for i := 0; i < n; i++ {
		r := results.At(i)
		if isError(r.Type()) {
			return f, fmt.Errorf("error must be the final result")
		}
		ts, err := m.TS(r.Type())
		if err != nil {
			return f, fmt.Errorf("result %d: %w", i, err)
		}
		f.Results = append(f.Results, Result{Type: r.Type(), TS: ts, IsInt64: tsmap.IsInt64(r.Type())})
	}

	return f, nil
}

// collectTypes drains the mapper's discovery accumulator, which grows while it
// is being read because emitting one struct can reveal more named types.
func collectTypes(pkg *packages.Package, base string, m *tsmap.Mapper, mod *Module, docs docIndex, classes classification) error {
	done := map[string]bool{}
	for {
		pending := m.Discovered()
		progress := false
		for _, n := range pending {
			name := n.Obj().Name()
			if done[name] {
				continue
			}
			done[name] = true
			progress = true

			// A class has no data shape: it crosses as a handle, so there is no
			// interface to declare and nothing for the codec to convert.
			if classes.isClass(n) {
				continue
			}

			doc := docs[n.Obj().Pos()]
			switch under := n.Underlying().(type) {
			case *types.Struct:
				s, err := buildStruct(pkg, n, under, m, docs)
				if err != nil {
					return fmt.Errorf("%s: type %s: %w", pos(pkg, base, n.Obj().Pos()), name, err)
				}
				s.Doc = doc
				mod.Structs = append(mod.Structs, s)

			case *types.Basic:
				if members := enumMembers(pkg, n); len(members) > 0 {
					mod.Enums = append(mod.Enums, Enum{Name: name, Doc: doc, Members: members})
					continue
				}
				ts, err := m.TS(under)
				if err != nil {
					return fmt.Errorf("%s: type %s: %w", pos(pkg, base, n.Obj().Pos()), name, err)
				}
				mod.Aliases = append(mod.Aliases, Alias{Name: name, Doc: doc, TS: ts})

			default:
				ts, err := m.TS(under)
				if err != nil {
					return fmt.Errorf("%s: type %s: %w", pos(pkg, base, n.Obj().Pos()), name, err)
				}
				mod.Aliases = append(mod.Aliases, Alias{Name: name, Doc: doc, TS: ts})
			}
		}
		if !progress {
			break
		}
	}
	return nil
}

func buildStruct(pkg *packages.Package, n *types.Named, st *types.Struct, m *tsmap.Mapper, docs docIndex) (Struct, error) {
	s := Struct{Name: n.Obj().Name(), Named: n}

	for i := 0; i < st.NumFields(); i++ {
		fld := st.Field(i)
		tag := reflect.StructTag(st.Tag(i))

		if fld.Embedded() {
			// An embedded struct with no json tag is inlined by encoding/json,
			// which maps cleanly onto `extends`.
			name, _, skip := tsmap.JSONFieldName(fld.Name(), tag.Lookup)
			if skip {
				continue
			}
			if _, tagged := tag.Lookup("json"); !tagged {
				if named, ok := types.Unalias(fld.Type()).(*types.Named); ok {
					if _, isStruct := named.Underlying().(*types.Struct); isStruct {
						ts, err := m.TS(named)
						if err != nil {
							return s, fmt.Errorf("embedded %s: %w", fld.Name(), err)
						}
						s.Extends = append(s.Extends, ts)
						continue
					}
				}
			}
			// Tagged embedded fields are encoded as a regular nested object.
			ts, err := m.TS(fld.Type())
			if err != nil {
				return s, fmt.Errorf("field %s: %w", fld.Name(), err)
			}
			s.Fields = append(s.Fields, Field{JSName: name, TS: ts, Type: fld.Type()})
			continue
		}

		if !fld.Exported() {
			continue
		}
		name, optional, skip := tsmap.JSONFieldName(fld.Name(), tag.Lookup)
		if skip {
			continue
		}
		ts, err := m.TS(fld.Type())
		if err != nil {
			return s, fmt.Errorf("field %s: %w", fld.Name(), err)
		}
		s.Fields = append(s.Fields, Field{
			JSName:   name,
			TS:       ts,
			Optional: optional,
			Doc:      docs[fld.Pos()],
			IsInt64:  tsmap.IsInt64(fld.Type()),
			Type:     fld.Type(),
		})
	}
	return s, nil
}

// enumMembers finds package-level constants declared with this named type.
// Their presence is what turns a `type Status string` into a real TypeScript
// literal union instead of a bare string alias.
func enumMembers(pkg *packages.Package, n *types.Named) []EnumMember {
	var out []EnumMember
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		c, ok := scope.Lookup(name).(*types.Const)
		if !ok || !c.Exported() {
			continue
		}
		named, ok := types.Unalias(c.Type()).(*types.Named)
		if !ok || named.Obj() != n.Obj() {
			continue
		}
		out = append(out, EnumMember{GoName: c.Name(), Literal: literal(c.Val())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GoName < out[j].GoName })
	return out
}

func literal(v constant.Value) string {
	if v.Kind() == constant.String {
		return strconv.Quote(constant.StringVal(v))
	}
	return v.ExactString()
}

// --- helpers ---

type docIndex map[token.Pos]string

func buildDocs(pkg *packages.Package) docIndex {
	idx := docIndex{}
	add := func(p token.Pos, g *ast.CommentGroup) {
		if g != nil {
			idx[p] = strings.TrimSpace(g.Text())
		}
	}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				add(d.Name.Pos(), d.Doc)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					doc := ts.Doc
					if doc == nil {
						doc = d.Doc
					}
					add(ts.Name.Pos(), doc)
					st, ok := ts.Type.(*ast.StructType)
					if !ok || st.Fields == nil {
						continue
					}
					for _, f := range st.Fields.List {
						g := f.Doc
						if g == nil {
							g = f.Comment
						}
						for _, nm := range f.Names {
							add(nm.Pos(), g)
						}
					}
				}
			}
		}
	}
	return idx
}

// pos renders a source position relative to base, so generated output is the
// same on every machine that builds it.
func pos(pkg *packages.Package, base string, p token.Pos) string {
	if pkg.Fset == nil {
		return "?"
	}
	position := pkg.Fset.Position(p)
	if base != "" {
		if r, err := filepath.Rel(base, position.Filename); err == nil && !strings.HasPrefix(r, "..") {
			position.Filename = r
		}
	}
	return position.String()
}

func isContext(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "context" && obj.Name() == "Context"
}

func isError(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	return named.Obj().Pkg() == nil && named.Obj().Name() == "error"
}

func paramName(p *types.Var, i int) string {
	if p.Name() != "" && p.Name() != "_" {
		return p.Name()
	}
	return fmt.Sprintf("arg%d", i)
}

func jsParamName(p *types.Var, i int) string {
	return tsmap.CamelCase(paramName(p, i))
}

// testEnv is the environment used to type-check, matching the compile target so
// build-tagged files resolve exactly as `go build` will resolve them.
func testEnv() []string {
	return append(os.Environ(), "GOOS=js", "GOARCH=wasm")
}

// structTagLookup adapts a raw struct tag to the lookup form JSONFieldName wants.
func structTagLookup(tag string) func(string) (string, bool) {
	return reflect.StructTag(tag).Lookup
}
