package scan

import (
	"encoding/base64"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/paulgrammer/gowasm/internal/tsmap"
)

// ExampleCall is one call to an exported function found inside a Go Example
// function, with every argument already rendered as JSON.
type ExampleCall struct {
	Example string   // ExampleExtractURLs
	GoFunc  string   // ExtractURLs
	JSFunc  string   // extractURLs
	Args    []string // JSON literals, one per parameter
	Pos     string
}

// Skipped records a call that could not be turned into a fixture, so the reason
// can be reported rather than silently dropped.
type Skipped struct {
	Example string
	GoFunc  string
	Pos     string
	Reason  string
}

// LoadExamples finds calls to mod's exported functions inside the package's
// Example functions.
//
// Rather than transpiling arbitrary Go to TypeScript, which is not tractable,
// only the call sites are extracted. Their expected results are recorded later
// by running the real Go code, so the fixtures are correct by construction.
func LoadExamples(opts Options, mod *Module) ([]ExampleCall, []Skipped, error) {
	pkgs, err := loadTestVariants(opts.Dir, opts.Pattern)
	if err != nil {
		return nil, nil, err
	}

	byGoName := map[string]Func{}
	for _, f := range mod.Funcs {
		byGoName[f.GoName] = f
	}

	var calls []ExampleCall
	var skipped []Skipped

	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Recv != nil || fd.Body == nil {
					continue
				}
				if !strings.HasPrefix(fd.Name.Name, "Example") {
					continue
				}
				c, s := walkExample(pkg, opts.Dir, fd, byGoName, mod)
				calls = append(calls, c...)
				skipped = append(skipped, s...)
			}
		}
	}

	sort.Slice(calls, func(i, j int) bool {
		if calls[i].Example != calls[j].Example {
			return calls[i].Example < calls[j].Example
		}
		return calls[i].Pos < calls[j].Pos
	})
	return calls, skipped, nil
}

func loadTestVariants(dir, pattern string) ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedSyntax | packages.NeedDeps | packages.NeedImports | packages.NeedFiles,
		Dir:   dir,
		Tests: true,
		Env:   testEnv(),
	}
	pkgs, err := packages.Load(cfg, pattern)
	if err != nil {
		return nil, fmt.Errorf("loading tests for %s: %w", pattern, err)
	}

	var out []*packages.Package
	for _, p := range pkgs {
		// Keep only the variants that actually carry test files: the plain
		// package has no Example functions in it.
		if p.TypesInfo == nil || !strings.Contains(p.ID, "[") {
			continue
		}
		if len(p.Errors) > 0 {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

func walkExample(pkg *packages.Package, base string, fd *ast.FuncDecl, byGoName map[string]Func, mod *Module) ([]ExampleCall, []Skipped) {
	var calls []ExampleCall
	var skipped []Skipped

	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident := calleeIdent(call.Fun)
		if ident == nil {
			return true
		}
		obj, ok := pkg.TypesInfo.Uses[ident].(*types.Func)
		if !ok {
			return true
		}
		f, exposed := byGoName[obj.Name()]
		if !exposed {
			return true
		}

		where := pos(pkg, base, call.Lparen)
		if mod.InvolvesClass(f) {
			skipped = append(skipped, Skipped{
				Example: fd.Name.Name, GoFunc: f.GoName, Pos: where,
				Reason: "a class, which has no literal form: the object lives in Go and only its handle crosses",
			})
			return true
		}
		args, err := renderArgs(pkg, call, f)
		if err != nil {
			skipped = append(skipped, Skipped{
				Example: fd.Name.Name, GoFunc: f.GoName, Pos: where, Reason: err.Error(),
			})
			return true
		}
		calls = append(calls, ExampleCall{
			Example: fd.Name.Name,
			GoFunc:  f.GoName,
			JSFunc:  f.JSName,
			Args:    args,
			Pos:     where,
		})
		return true
	})

	return calls, skipped
}

func calleeIdent(fun ast.Expr) *ast.Ident {
	switch f := fun.(type) {
	case *ast.Ident: // in-package example
		return f
	case *ast.SelectorExpr: // urls.ExtractURLs from an _test package
		return f.Sel
	default:
		return nil
	}
}

func renderArgs(pkg *packages.Package, call *ast.CallExpr, f Func) ([]string, error) {
	// A context parameter is not part of the JS signature, so the Go call has
	// one more argument than the fixture needs.
	offset := 0
	if f.HasContext {
		offset = 1
	}
	want := len(f.Params)
	got := len(call.Args) - offset

	if f.Variadic {
		// The client packs the rest arguments into a single array before they
		// cross the boundary, so the fixture has to do the same.
		fixed := want - 1
		if got < fixed {
			return nil, fmt.Errorf("call passes %d argument(s), signature needs at least %d", got, fixed)
		}
		if call.Ellipsis.IsValid() {
			// f(xs...) forwards an existing slice; treat it as one argument.
			return literalArgs(pkg, call.Args[offset:])
		}
		out, err := literalArgs(pkg, call.Args[offset:offset+fixed])
		if err != nil {
			return nil, err
		}
		rest, err := literalArgs(pkg, call.Args[offset+fixed:])
		if err != nil {
			return nil, err
		}
		return append(out, "["+strings.Join(rest, ",")+"]"), nil
	}

	if got != want {
		return nil, fmt.Errorf("call passes %d argument(s), signature has %d", got, want)
	}
	return literalArgs(pkg, call.Args[offset:])
}

func literalArgs(pkg *packages.Package, exprs []ast.Expr) ([]string, error) {
	out := make([]string, 0, len(exprs))
	for i, e := range exprs {
		lit, err := jsonLiteral(pkg, e)
		if err != nil {
			return nil, fmt.Errorf("argument %d: %w", i+1, err)
		}
		out = append(out, lit)
	}
	return out, nil
}

// jsonLiteral renders a Go expression as JSON, when it can be known statically.
//
// Constant folding comes from the type checker, so a named constant such as
// urls.Relaxed resolves to its value without any string manipulation.
func jsonLiteral(pkg *packages.Package, expr ast.Expr) (string, error) {
	if tv, ok := pkg.TypesInfo.Types[expr]; ok && tv.Value != nil {
		return constantJSON(tv.Value)
	}

	switch e := expr.(type) {
	case *ast.CompositeLit:
		return compositeJSON(pkg, e)
	case *ast.CallExpr:
		// A conversion such as []byte("hi") is still statically known.
		if len(e.Args) == 1 {
			if tv, ok := pkg.TypesInfo.Types[e.Fun]; ok && tv.IsType() {
				return conversionJSON(pkg, tv.Type, e.Args[0])
			}
		}
	case *ast.Ident:
		if e.Name == "nil" {
			return "null", nil
		}
	case *ast.UnaryExpr:
		// &Match{...} encodes exactly like the value it points at.
		if e.Op.String() == "&" {
			return jsonLiteral(pkg, e.X)
		}
	}
	return "", fmt.Errorf("not a literal value")
}

func constantJSON(v constant.Value) (string, error) {
	switch v.Kind() {
	case constant.String:
		return strconv.Quote(constant.StringVal(v)), nil
	case constant.Bool:
		return strconv.FormatBool(constant.BoolVal(v)), nil
	case constant.Int:
		return v.ExactString(), nil
	case constant.Float:
		f, _ := constant.Float64Val(v)
		return strconv.FormatFloat(f, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("unsupported constant %s", v.String())
	}
}

func conversionJSON(pkg *packages.Package, to types.Type, arg ast.Expr) (string, error) {
	// []byte("hi") crosses the boundary base64-encoded, matching encoding/json.
	if s, ok := types.Unalias(to).(*types.Slice); ok && isByteType(s.Elem()) {
		tv, ok := pkg.TypesInfo.Types[arg]
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
			return "", fmt.Errorf("not a literal value")
		}
		return strconv.Quote(base64Of(constant.StringVal(tv.Value))), nil
	}
	return jsonLiteral(pkg, arg)
}

func compositeJSON(pkg *packages.Package, lit *ast.CompositeLit) (string, error) {
	tv, ok := pkg.TypesInfo.Types[lit]
	if !ok {
		return "", fmt.Errorf("not a literal value")
	}

	switch under := tv.Type.Underlying().(type) {
	case *types.Slice, *types.Array:
		parts := make([]string, 0, len(lit.Elts))
		for _, el := range lit.Elts {
			s, err := jsonLiteral(pkg, el)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ",") + "]", nil

	case *types.Map:
		parts := make([]string, 0, len(lit.Elts))
		for _, el := range lit.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				return "", fmt.Errorf("not a literal value")
			}
			k, err := jsonLiteral(pkg, kv.Key)
			if err != nil {
				return "", err
			}
			v, err := jsonLiteral(pkg, kv.Value)
			if err != nil {
				return "", err
			}
			// JSON object keys are always strings, even when the Go key is not.
			if !strings.HasPrefix(k, `"`) {
				k = strconv.Quote(strings.Trim(k, `"`))
			}
			parts = append(parts, k+":"+v)
		}
		sort.Strings(parts)
		return "{" + strings.Join(parts, ",") + "}", nil

	case *types.Struct:
		return structJSON(pkg, lit, under)

	default:
		return "", fmt.Errorf("not a literal value")
	}
}

func structJSON(pkg *packages.Package, lit *ast.CompositeLit, st *types.Struct) (string, error) {
	byName := map[string]int{}
	for i := 0; i < st.NumFields(); i++ {
		byName[st.Field(i).Name()] = i
	}

	var parts []string
	for idx, el := range lit.Elts {
		field := idx
		value := el
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				return "", fmt.Errorf("not a literal value")
			}
			i, known := byName[key.Name]
			if !known {
				return "", fmt.Errorf("unknown field %s", key.Name)
			}
			field, value = i, kv.Value
		}
		if field >= st.NumFields() {
			return "", fmt.Errorf("too many fields")
		}

		f := st.Field(field)
		if !f.Exported() {
			continue
		}
		name, _, skip := tsmap.JSONFieldName(f.Name(), structTagLookup(st.Tag(field)))
		if skip {
			continue
		}
		v, err := jsonLiteral(pkg, value)
		if err != nil {
			return "", err
		}
		parts = append(parts, strconv.Quote(name)+":"+v)
	}
	return "{" + strings.Join(parts, ",") + "}", nil
}

func isByteType(t types.Type) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

func base64Of(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
