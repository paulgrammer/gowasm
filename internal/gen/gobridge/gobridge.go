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
	GoName  string
	JSName  string
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

// Generate renders the bridge source for mod.
func Generate(mod *scan.Module, namespace string) ([]byte, error) {
	imp := newImports(mod.PkgPath)
	d := data{Namespace: namespace}

	for _, f := range mod.Funcs {
		g, err := renderFunc(f, imp)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", f.Pos, f.GoName, err)
		}
		d.Funcs = append(d.Funcs, g)
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

func renderFunc(f scan.Func, imp *imports) (fn, error) {
	g := fn{
		GoName: f.GoName,
		JSName: f.JSName,
		Doc:    firstLine(f.Doc),
		Arity:  len(f.Params),
	}

	args := make([]string, 0, len(f.Params)+1)
	if f.HasContext {
		imp.add("context")
		args = append(args, "context.Background()")
	}

	for i, p := range f.Params {
		goType := types.TypeString(p.Type, imp.qualifier)
		g.Decodes = append(g.Decodes,
			fmt.Sprintf("var p%d %s", i, goType),
			fmt.Sprintf("if err := json.Unmarshal(a[%d], &p%d); err != nil {", i, i),
			fmt.Sprintf("\treturn nil, fmt.Errorf(%q, err)",
				fmt.Sprintf("%s: argument %d (%s): %%w", f.JSName, i+1, p.GoName)),
			"}",
		)
		arg := fmt.Sprintf("p%d", i)
		if f.Variadic && i == len(f.Params)-1 {
			arg += "..."
		}
		args = append(args, arg)
	}

	imp.addAlias("userpkg", imp.userPath)
	call := fmt.Sprintf("userpkg.%s(%s)", f.GoName, strings.Join(args, ", "))

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

// --- import bookkeeping ---

type imports struct {
	userPath string
	byPath   map[string]string // path -> alias ("" means use the package name)
}

func newImports(userPath string) *imports {
	i := &imports{userPath: userPath, byPath: map[string]string{}}
	// Always needed by the emitted code.
	i.add("encoding/json")
	i.add("fmt")
	i.add("os")
	return i
}

func (i *imports) add(path string)             { i.addAlias("", path) }
func (i *imports) addAlias(alias, path string) { i.byPath[path] = alias }

// qualifier names a package inside generated source, registering the import as
// a side effect so the import block always matches what the body references.
func (i *imports) qualifier(p *types.Package) string {
	if p == nil {
		return ""
	}
	if p.Path() == i.userPath {
		i.addAlias("userpkg", p.Path())
		return "userpkg"
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
