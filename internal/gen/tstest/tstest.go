// Package tstest emits the generated node:test suites.
package tstest

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/paulgrammer/gowasm/internal/fixtures"
	"github.com/paulgrammer/gowasm/internal/scan"
)

//go:embed templates/*.tmpl
var tmplFS embed.FS

// File is one emitted test file, relative to the package's test directory.
type File struct {
	Path    string
	Content []byte
}

type fnView struct {
	JSName string
	Params []scan.Param
	// Required is the count of parameters a caller must supply. A variadic tail
	// may legitimately be empty, so it does not count.
	Required int

	// SampleArgs is a syntactically valid call for this function.
	SampleArgs string
	// ExtraArgs is SampleArgs plus one argument too many: always a type error.
	ExtraArgs string
	// BadArg is a value of the wrong type for the first parameter, or empty
	// when that parameter is too loosely typed for a mismatch to be an error.
	BadArg string
	// RestArgs completes a call whose first argument is BadArg.
	RestArgs string
	// ReturnGuard records that the return type is narrow enough to assert on.
	ReturnGuard bool
}

// Fixture is one recorded example call, in the shape the template renders.
type Fixture struct {
	Example      string
	Pos          string
	JSFunc       string
	Title        string
	Args         string
	Result       string
	ErrorLiteral string
	Error        bool
	Void         bool
}

// GenerateFixtures renders the behavioural suite recorded from Go Examples.
func GenerateFixtures(mod *scan.Module, recorded []fixtures.Fixture) ([]File, error) {
	if len(recorded) == 0 {
		return nil, nil
	}

	params := map[string][]scan.Param{}
	results := map[string][]scan.Result{}
	variadic := map[string]bool{}
	for _, f := range mod.Funcs {
		params[f.JSName] = f.Params
		results[f.JSName] = f.Results
		variadic[f.JSName] = f.Variadic
	}
	lit := newLiteralizer(mod)
	// Literal unions are the one place a recorded argument can be well-formed
	// JSON and still not a member of its TypeScript type: an Example that
	// deliberately passes an invalid enum value, to show what the error is.
	enums := map[string]map[string]bool{}
	for _, e := range mod.Enums {
		members := map[string]bool{}
		for _, m := range e.Members {
			members[m.Literal] = true
		}
		enums[e.Name] = members
	}

	casts := map[string]bool{}
	seen := map[string]int{}
	views := make([]Fixture, 0, len(recorded))
	for _, f := range recorded {
		// Several calls in one Example need distinct test names.
		seen[f.Example]++
		title := f.Example
		if n := seen[f.Example]; n > 1 || countCalls(recorded, f.Example) > 1 {
			title = fmt.Sprintf("%s: call %d", f.Example, n)
		}

		v := Fixture{
			Example: f.Example,
			Pos:     f.Pos,
			JSFunc:  f.JSFunc,
			Title:   strconv.Quote(title + " -> " + f.JSFunc),
			Args:    renderArgs(lit, f.Args, params[f.JSFunc], variadic[f.JSFunc], enums, casts),
			Result:  renderResult(lit, f.Result, results[f.JSFunc]),
			Void:    f.Void,
		}
		if f.Error != "" {
			v.Error = true
			v.ErrorLiteral = strconv.Quote(f.Error)
		}
		if v.Result == "" && !v.Void && !v.Error {
			v.Result = "undefined"
		}
		views = append(views, v)
	}

	imports := make([]string, 0, len(casts))
	for name := range casts {
		imports = append(imports, name)
	}
	sort.Strings(imports)

	b, err := render("fixtures.ts.tmpl", map[string]any{
		"Fixtures":    views,
		"TypeImports": imports,
		"UsesBytes":   lit.usesBytes,
	})
	if err != nil {
		return nil, err
	}
	return []File{{Path: "fixtures.gen.test.ts", Content: b}}, nil
}

// renderArgs writes the recorded arguments as TypeScript, casting any value
// that its parameter's type does not admit. The cast keeps the call intact
// while making it obvious the value is deliberately out of domain.
func renderArgs(lit *literalizer, args []string, params []scan.Param, isVariadic bool, enums map[string]map[string]bool, casts map[string]bool) string {
	out := make([]string, 0, len(args))
	for i, a := range args {
		if i < len(params) {
			if members, isEnum := enums[params[i].TS]; isEnum && !members[a] {
				out = append(out, fmt.Sprintf("%s as %s", a, params[i].TS))
				casts[params[i].TS] = true
				continue
			}
			rendered := lit.render(params[i].Type, a)
			// The recorded form packs a variadic tail into one array, but the
			// TypeScript signature takes a rest parameter, so spread it back.
			if isVariadic && i == len(params)-1 {
				rendered = "..." + rendered
			}
			a = rendered
		}
		out = append(out, a)
	}
	return strings.Join(out, ", ")
}

// renderResult writes the expected value in terms of the public API types, so a
// binary result is compared as a typed array rather than as base64.
func renderResult(lit *literalizer, raw string, results []scan.Result) string {
	if raw == "" {
		return ""
	}
	switch len(results) {
	case 0:
		return raw
	case 1:
		return lit.render(results[0].Type, raw)
	default:
		// Several results arrive as a tuple; render each against its own type.
		var parts []json.RawMessage
		if err := json.Unmarshal([]byte(raw), &parts); err != nil || len(parts) != len(results) {
			return raw
		}
		rendered := make([]string, 0, len(parts))
		for i, p := range parts {
			rendered = append(rendered, lit.render(results[i].Type, string(p)))
		}
		return "[" + strings.Join(rendered, ", ") + "]"
	}
}

func countCalls(all []fixtures.Fixture, example string) int {
	n := 0
	for _, f := range all {
		if f.Example == example {
			n++
		}
	}
	return n
}

// Generate renders the test suites for mod.
func Generate(mod *scan.Module, targets []string) ([]File, error) {
	if len(mod.Funcs) == 0 {
		return nil, fmt.Errorf("no functions to test")
	}

	views := make([]fnView, 0, len(mod.Funcs))
	for _, f := range mod.Funcs {
		views = append(views, buildView(f))
	}
	data := map[string]any{"Funcs": views, "First": views[0]}
	// The rejection half of the concurrency test needs a function that actually
	// requires an argument; a package of niladic functions simply skips it.
	for _, v := range views {
		if v.Required > 0 {
			data["FirstRequired"] = v
			break
		}
	}

	var files []File
	if hasTarget(targets, "node") {
		b, err := render("contract.ts.tmpl", data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "contract.gen.test.ts", Content: b})
	}
	if hasTarget(targets, "browser") {
		b, err := render("browser.ts.tmpl", data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "browser-loader.gen.test.ts", Content: b})
	}
	return files, nil
}

func buildView(f scan.Func) fnView {
	v := fnView{JSName: f.JSName, Params: f.Params, Required: len(f.Params)}
	if f.Variadic && v.Required > 0 {
		v.Required--
	}

	args := make([]string, 0, len(f.Params))
	for _, p := range f.Params {
		args = append(args, sampleValue(p.TS))
	}
	v.SampleArgs = strings.Join(args, ", ")

	extra := append(append([]string{}, args...), `"extra"`)
	v.ExtraArgs = strings.Join(extra, ", ")

	if len(f.Params) > 0 {
		if bad := wrongTypedValue(f.Params[0].TS); bad != "" {
			v.BadArg = bad
			if len(args) > 1 {
				v.RestArgs = ", " + strings.Join(args[1:], ", ")
			}
		}
	}

	// A `string` or `unknown` return cannot be distinguished from a regression
	// to `any` by assigning it to a string, so no guard is emitted for those.
	switch f.TSReturn() {
	case "string", "unknown", "ISODateTime":
	default:
		v.ReturnGuard = true
	}
	return v
}

// sampleValue produces a well-typed value for a TS type. It only has to
// type-check; these calls are never expected to succeed semantically.
func sampleValue(ts string) string {
	switch {
	case ts == "string", ts == "ISODateTime":
		return `""`
	case ts == "Uint8Array":
		return "new Uint8Array()"
	case ts == "number":
		return "0"
	case ts == "boolean":
		return "false"
	case ts == "unknown":
		return "null"
	case strings.HasPrefix(ts, `"`):
		// A literal union: its first member is the only safe choice.
		if end := strings.Index(ts[1:], `"`); end >= 0 {
			return ts[:end+2]
		}
		return `""`
	case strings.HasSuffix(ts, "[]"):
		return "[]"
	case strings.HasPrefix(ts, "Record<"):
		return "{}"
	case strings.HasSuffix(ts, "| null"):
		return "null"
	default:
		// A struct parameter. These guards are about arity and primitives, not
		// object shapes, so sidestep the shape entirely.
		return "undefined as never"
	}
}

// wrongTypedValue returns a literal that is definitely invalid for ts, or ""
// when no such literal exists.
func wrongTypedValue(ts string) string {
	switch {
	case ts == "string", ts == "ISODateTime":
		return "12345"
	case ts == "Uint8Array":
		return "12345"
	case ts == "number":
		return `"not a number"`
	case ts == "boolean":
		return `"not a boolean"`
	case strings.HasPrefix(ts, `"`):
		return `"not a member of this union"`
	case strings.HasSuffix(ts, "[]"):
		return `"not an array"`
	case strings.HasPrefix(ts, "Record<"):
		return `"not an object"`
	default:
		return ""
	}
}

func hasTarget(targets []string, want string) bool {
	for _, t := range targets {
		if t == want {
			return true
		}
	}
	return false
}

func render(name string, data any) ([]byte, error) {
	t, err := template.New(name).ParseFS(tmplFS, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering %s: %w", name, err)
	}
	return buf.Bytes(), nil
}
