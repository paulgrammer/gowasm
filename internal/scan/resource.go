package scan

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/paulgrammer/gowasm/internal/tsmap"
)

// Deciding which Go types become TypeScript classes.
//
// A named struct becomes a class when two things are true: it has exported
// methods worth calling, and it is only ever seen behind a pointer. Both
// conditions are load-bearing. examples/urls returns *Match from one function
// and []Match from another, and Match is plain data -- keying on pointerness
// alone would have turned it into an opaque handle and broken every consumer.
//
// This runs as its own pass, before anything is built, because the question is
// not answerable inside collectTypes. That loop discovers types as it drains,
// so a value use can surface long after the decision, and worse, it can be
// circular:
//
//	type Store struct { Parent *Store; Meta Meta }
//	type Meta  struct { Origin Store }   // reachable only through Store's fields
//
// If Store is a handle its fields are never walked, Origin is never seen, and
// it stays a handle. If it is data the value use demotes it. The answer would
// depend on the answer. Here the walk descends into every struct regardless of
// what it is about to decide, so the result depends only on the source.

// conventionalMethods are the methods Go programmers attach to plain data.
// Finding one says nothing about whether a type has identity, so they do not
// make a type a candidate.
//
// Without this list, adding a debug String() to a Point would turn
// `interface Point { x: number; y: number }` into a fieldless opaque handle and
// break every consumer -- a published API broken by a change that could not
// look more harmless.
var conventionalMethods = map[string]bool{
	"String": true, "GoString": true, "Error": true, "Format": true,
	"MarshalJSON": true, "UnmarshalJSON": true,
	"MarshalText": true, "UnmarshalText": true,
	"MarshalBinary": true, "UnmarshalBinary": true,
	"Is": true, "As": true, "Unwrap": true, "Equal": true,
}

// reservedMethods cannot be generated as class members.
//
// `then` is the dangerous one: a class with a then() is a thenable, so `await
// store` would call it and returning one from an async function would recurse
// forever. The others would collide with what the class already provides.
var reservedMethods = map[string]string{
	"then":        "a then() method would make the class a thenable, so `await` on it would call the method instead of resolving",
	"close":       "the generated class provides close() for releasing the handle",
	"constructor": "constructor is not a callable member of a class",
}

// directives are the escape hatch for a type the walk refuses to guess about.
const (
	directiveResource = "gowasm:resource"
	directiveData     = "gowasm:data"
)

// classification is the outcome of the pass: which named types are classes,
// and which candidates were passed over.
type classification struct {
	classes map[*types.TypeName]*types.Named
	// demoted names candidates that are only ever used as values, so their
	// methods are not reachable. Reported rather than dropped in silence.
	demoted []string
}

func (c classification) isClass(n *types.Named) bool {
	if n == nil {
		return false
	}
	_, ok := c.classes[n.Obj()]
	return ok
}

// occurrence records where a named type was seen.
type occurrence struct {
	// handle marks a whole *T parameter or result: the one position a handle
	// can occupy.
	handle bool
	// value marks anything else -- a bare T, a []*T, a field of a data struct.
	// All of them require the type to be JSON, which a handle is not.
	value bool
	// where remembers one position of each kind, so an error can point at
	// something rather than merely assert.
	handleAt string
	valueAt  string
}

// classify decides which types are classes, from the exported surface alone.
func classify(pkg *packages.Package, dir string) (classification, error) {
	out := classification{classes: map[*types.TypeName]*types.Named{}}

	directives := buildDirectives(pkg)
	scope := pkg.Types.Scope()

	// Candidacy depends only on a type's own method set, so it is answerable
	// before any walking and cannot be circular.
	candidates := map[*types.TypeName]*types.Named{}
	names := scope.Names()
	sort.Strings(names)
	for _, name := range names {
		obj, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || !obj.Exported() {
			continue
		}
		named, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}
		if _, isStruct := named.Underlying().(*types.Struct); !isStruct {
			continue
		}
		switch directives[obj] {
		case directiveData:
			continue
		case directiveResource:
			candidates[obj] = named
			continue
		}
		if hasOwnMethods(named) {
			candidates[obj] = named
		}
	}

	w := &occurrenceWalk{
		pkg:        pkg,
		dir:        dir,
		candidates: candidates,
		seen:       map[occKey]bool{},
		found:      map[*types.TypeName]*occurrence{},
	}

	for _, name := range names {
		fn, ok := scope.Lookup(name).(*types.Func)
		if !ok || !fn.Exported() {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Recv() != nil {
			continue
		}
		w.signature(sig, pos(pkg, dir, fn.Pos())+": "+fn.Name())
	}

	// Sorted so the same package always reports the same problem first.
	objs := make([]*types.TypeName, 0, len(candidates))
	for obj := range candidates {
		objs = append(objs, obj)
	}
	sort.Slice(objs, func(i, j int) bool { return objs[i].Name() < objs[j].Name() })

	for _, obj := range objs {
		occ := w.found[obj]
		forced := directives[obj] == directiveResource
		switch {
		case occ == nil:
			// Declared but never reachable from the exported surface. Nothing
			// to generate and nothing to complain about.
		case occ.handle && occ.value:
			return out, fmt.Errorf(
				"%s has methods and is used both ways, so it cannot be both a class and plain data:\n"+
					"  as a pointer at %s\n"+
					"  by value at    %s\n"+
					"  use *%s everywhere to make it a class, or add //%s above the type to keep it plain data",
				obj.Name(), occ.handleAt, occ.valueAt, obj.Name(), directiveData)
		case occ.handle:
			out.classes[obj] = candidates[obj]
		case forced:
			return out, fmt.Errorf(
				"%s is marked //%s but is used by value at %s; a class cannot cross the boundary as data",
				obj.Name(), directiveResource, occ.valueAt)
		default:
			out.demoted = append(out.demoted, obj.Name())
		}
	}

	for obj, named := range out.classes {
		if err := checkClassMembers(pkg, dir, obj, named); err != nil {
			return out, err
		}
	}
	return out, nil
}

// hasOwnMethods reports whether a type declares an exported method that says
// something about identity.
//
// Declared methods only, never types.NewMethodSet: a promoted Lock/Unlock from
// an embedded *sync.Mutex would otherwise become callable from JavaScript, and
// a lock() with no matching unlock() would wedge the instance for good.
func hasOwnMethods(n *types.Named) bool {
	for i := 0; i < n.NumMethods(); i++ {
		m := n.Method(i)
		if m.Exported() && !conventionalMethods[m.Name()] {
			return true
		}
	}
	return false
}

// checkClassMembers refuses the members that cannot be generated, before
// anything downstream tries.
func checkClassMembers(pkg *packages.Package, dir string, obj *types.TypeName, n *types.Named) error {
	seen := map[string]string{}
	note := func(js, goName string) error {
		if why, bad := reservedMethods[js]; bad {
			return fmt.Errorf("%s: %s.%s maps to %q, which cannot be generated: %s",
				pos(pkg, dir, obj.Pos()), obj.Name(), goName, js, why)
		}
		if prev, dup := seen[js]; dup {
			return fmt.Errorf("%s: %s.%s and %s.%s both map to %q; rename one",
				pos(pkg, dir, obj.Pos()), obj.Name(), prev, obj.Name(), goName, js)
		}
		seen[js] = goName
		return nil
	}

	for i := 0; i < n.NumMethods(); i++ {
		m := n.Method(i)
		if !m.Exported() || conventionalMethods[m.Name()] {
			continue
		}
		// Close is folded into the generated close() rather than refused; it is
		// the single most likely method on the types this feature targets.
		if m.Name() == "Close" {
			continue
		}
		if err := note(tsmap.CamelCase(m.Name()), m.Name()); err != nil {
			return err
		}
	}

	st, _ := n.Underlying().(*types.Struct)
	if st == nil {
		return nil
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Exported() {
			continue
		}
		name, _, skip := tsmap.JSONFieldName(f.Name(), structTagLookup(st.Tag(i)))
		if skip {
			continue
		}
		// Go forbids a field and a method sharing a name, so the getter cannot
		// collide with a real method. The generated setter can.
		if err := note(name, f.Name()); err != nil {
			return err
		}
		if err := note(SetterName(name), f.Name()); err != nil {
			return err
		}
	}
	return nil
}

// --- the walk ---

type occKey struct {
	obj    *types.TypeName
	nested bool
}

type occurrenceWalk struct {
	pkg        *packages.Package
	dir        string
	candidates map[*types.TypeName]*types.Named
	seen       map[occKey]bool
	found      map[*types.TypeName]*occurrence
}

func (w *occurrenceWalk) record(obj *types.TypeName, handle bool, at string) {
	o := w.found[obj]
	if o == nil {
		o = &occurrence{}
		w.found[obj] = o
	}
	if handle {
		o.handle = true
		if o.handleAt == "" {
			o.handleAt = at
		}
		return
	}
	o.value = true
	if o.valueAt == "" {
		o.valueAt = at
	}
}

func (w *occurrenceWalk) signature(sig *types.Signature, at string) {
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		w.top(params.At(i).Type(), at)
	}
	results := sig.Results()
	for i := 0; i < results.Len(); i++ {
		w.top(results.At(i).Type(), at)
	}
}

// top walks a whole parameter or result: the only position a handle may
// occupy.
func (w *occurrenceWalk) top(t types.Type, at string) {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		if n, ok := types.Unalias(p.Elem()).(*types.Named); ok {
			if _, isStruct := n.Underlying().(*types.Struct); isStruct {
				w.record(n.Obj(), true, at)
				w.members(n, at)
				return
			}
		}
	}
	w.nested(t, at)
}

// nested walks everything else. A named struct found here has to be JSON, and
// a handle is not.
func (w *occurrenceWalk) nested(t types.Type, at string) {
	switch u := types.Unalias(t).(type) {
	case *types.Pointer:
		w.nested(u.Elem(), at)
	case *types.Slice:
		w.nested(u.Elem(), at)
	case *types.Array:
		w.nested(u.Elem(), at)
	case *types.Map:
		w.nested(u.Key(), at)
		w.nested(u.Elem(), at)
	case *types.Named:
		if _, isStruct := u.Underlying().(*types.Struct); isStruct {
			w.record(u.Obj(), false, at)
		}
		w.members(u, at)
	}
}

// members walks what a named type leads to.
//
// The distinction that keeps this decidable: a candidate's fields are
// prospective accessors, each its own top-level position, so a *Store field on
// a class is not a demotion. A plain struct's fields are data, so everything
// they reach is a value use. Neither branch consults the classification, only
// the method set, which is fixed before the walk begins.
func (w *occurrenceWalk) members(n *types.Named, at string) {
	obj := n.Obj()
	_, candidate := w.candidates[obj]

	key := occKey{obj: obj, nested: !candidate}
	if w.seen[key] {
		return
	}
	w.seen[key] = true

	if candidate {
		for i := 0; i < n.NumMethods(); i++ {
			m := n.Method(i)
			if !m.Exported() || conventionalMethods[m.Name()] {
				continue
			}
			if sig, ok := m.Type().(*types.Signature); ok {
				w.signature(sig, at+" -> "+obj.Name()+"."+m.Name())
			}
		}
	}

	st, ok := n.Underlying().(*types.Struct)
	if !ok {
		return
	}
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Exported() && !f.Embedded() {
			continue
		}
		where := at + " -> " + obj.Name() + "." + f.Name()
		if candidate {
			w.top(f.Type(), where)
		} else {
			w.nested(f.Type(), where)
		}
	}
}

// SetterName is the writer generated for a field getter: name -> setName.
func SetterName(js string) string {
	if js == "" {
		return "set"
	}
	return "set" + strings.ToUpper(js[:1]) + js[1:]
}

// --- directives ---

// buildDirectives finds //gowasm:resource and //gowasm:data above a type.
//
// The raw comment text is scanned rather than the parsed doc, because
// ast.CommentGroup.Text strips exactly this shape of comment -- the same
// treatment it gives //go:build.
func buildDirectives(pkg *packages.Package) map[*types.TypeName]string {
	out := map[*types.TypeName]string{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				doc := ts.Doc
				if doc == nil {
					doc = gd.Doc
				}
				d := readDirective(doc)
				if d == "" {
					continue
				}
				obj, ok := pkg.TypesInfo.Defs[ts.Name].(*types.TypeName)
				if ok {
					out[obj] = d
				}
			}
		}
	}
	return out
}

func readDirective(g *ast.CommentGroup) string {
	if g == nil {
		return ""
	}
	for _, c := range g.List {
		text := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		switch text {
		case directiveResource:
			return directiveResource
		case directiveData:
			return directiveData
		}
	}
	return ""
}

// buildClass turns a classified named type into the IR the generators consume.
//
// The method signatures go through buildFunc unchanged: sig.Params() on a
// method excludes the receiver, so the context strip, the variadic handling and
// the trailing-error rule are already correct for a method as written.
func buildClass(pkg *packages.Package, base string, n *types.Named, m *tsmap.Mapper, docs docIndex, classes classification, mod *Module) (Class, error) {
	obj := n.Obj()
	c := Class{Name: obj.Name(), Doc: docs[obj.Pos()], Named: n}

	for i := 0; i < n.NumMethods(); i++ {
		fn := n.Method(i)
		if !fn.Exported() || conventionalMethods[fn.Name()] {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok {
			continue
		}

		// Close is not a method like the others: the generated close() calls it
		// and then releases the handle, so `await using` closes the resource
		// rather than merely forgetting about it.
		if fn.Name() == "Close" {
			if !isPlainCloser(sig) {
				return c, fmt.Errorf("%s: %s.Close must be `Close() error` to be folded into the generated close(); rename it",
					pos(pkg, base, fn.Pos()), c.Name)
			}
			c.HasGoClose = true
			continue
		}

		f, err := buildFunc(pkg, base, fn, sig, m, docs)
		if err != nil {
			return c, fmt.Errorf("%s: %s.%s: %w", pos(pkg, base, fn.Pos()), c.Name, fn.Name(), err)
		}
		f.Recv = c.Name
		c.Methods = append(c.Methods, f)
	}

	st, ok := n.Underlying().(*types.Struct)
	if !ok {
		return c, nil
	}
	for i := 0; i < st.NumFields(); i++ {
		fld := st.Field(i)
		if !fld.Exported() {
			continue
		}
		name, optional, skip := tsmap.JSONFieldName(fld.Name(), structTagLookup(st.Tag(i)))
		if skip {
			continue
		}

		// A field holding another resource would have to travel as a handle
		// inside a JSON value, which is exactly what this design keeps apart.
		// Refusing the whole type would reject an ordinary Go shape, so the
		// field is left off and said out loud; a method is the way to reach it.
		if inner, isClass := classFieldType(fld.Type(), classes); isClass {
			mod.Notes = append(mod.Notes, fmt.Sprintf(
				"%s.%s holds %s, which is a class, so it has no accessor; expose it with a method",
				c.Name, fld.Name(), inner))
			continue
		}

		ts, err := m.TS(fld.Type())
		if err != nil {
			return c, fmt.Errorf("%s: %s.%s: %w", pos(pkg, base, fld.Pos()), c.Name, fld.Name(), err)
		}
		c.Fields = append(c.Fields, Accessor{
			GoName:   fld.Name(),
			JSName:   name,
			SetName:  SetterName(name),
			Doc:      docs[fld.Pos()],
			Type:     fld.Type(),
			TS:       ts,
			IsInt64:  tsmap.IsInt64(fld.Type()),
			Optional: optional,
		})
	}
	return c, nil
}

// isPlainCloser reports whether a signature is exactly `Close() error`.
func isPlainCloser(sig *types.Signature) bool {
	return sig.Params().Len() == 0 &&
		sig.Results().Len() == 1 &&
		isError(sig.Results().At(0).Type())
}

// classFieldType reports whether a field's type is, or contains at its head, a
// class.
func classFieldType(t types.Type, classes classification) (string, bool) {
	switch u := types.Unalias(t).(type) {
	case *types.Pointer:
		return classFieldType(u.Elem(), classes)
	case *types.Slice:
		return classFieldType(u.Elem(), classes)
	case *types.Array:
		return classFieldType(u.Elem(), classes)
	case *types.Map:
		return classFieldType(u.Elem(), classes)
	case *types.Named:
		if classes.isClass(u) {
			return u.Obj().Name(), true
		}
	}
	return "", false
}
