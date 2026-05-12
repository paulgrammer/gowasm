// Package tsclient emits every TypeScript file in the generated npm package:
// the types, the typed client, the loader runtime and both entry points.
package tsclient

import (
	"bytes"
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"unicode"

	"github.com/paulgrammer/gowasm/internal/scan"
	"github.com/paulgrammer/gowasm/internal/tsmap"
)

//go:embed templates/*.tmpl
var tmplFS embed.FS

// File is one emitted file, at a path relative to the package's src directory.
type File struct {
	Path    string
	Content []byte
}

// Options controls emission.
type Options struct {
	// Namespace prefixes the globals each instance installs itself under.
	Namespace string
	// Targets selects which entry points to emit: "node", "browser", or both.
	Targets []string
}

// ReservedNames lists the exported functions whose JavaScript name cannot be a
// bare named export, so callers can be told rather than left to wonder.
func ReservedNames(mod *scan.Module) []string {
	var out []string
	for _, f := range mod.Funcs {
		if reservedJS[f.JSName] {
			out = append(out, f.GoName+" -> "+f.JSName)
		}
	}
	sort.Strings(out)
	return out
}

// Generate renders every TypeScript file for mod.
func Generate(mod *scan.Module, opts Options) ([]File, error) {
	api := apiName(mod.PkgName)

	files := []File{}

	// Static runtime pieces: identical for every package.
	for _, s := range []struct{ tmpl, path string }{
		{"core.ts.tmpl", "runtime/core.ts"},
		{"wasm_exec.d.ts.tmpl", "vendor/wasm_exec.d.ts"},
	} {
		b, err := render(s.tmpl, nil)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: s.path, Content: b})
	}

	types, err := render("types.ts.tmpl", mod)
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: "generated/types.ts", Content: types})

	cdc := newCodec(mod)
	useCodec := cdc.Needed(mod)
	funcs := renderFuncs(mod, cdc)
	if useCodec {
		views, usesMapValues := buildCodecViews(cdc, mod)
		b, err := render("codec.ts.tmpl", map[string]any{
			"Structs":       views,
			"UsesMapValues": usesMapValues,
		})
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "generated/codec.ts", Content: b})
	}

	client, err := render("client.ts.tmpl", map[string]any{
		"PkgName":      mod.PkgName,
		"APIName":      api,
		"Funcs":        funcs,
		"TypeImports":  typeImports(mod, funcs),
		"CodecImports": codecImports(cdc, mod, useCodec),
	})
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: "generated/client.ts", Content: client})

	for _, target := range opts.Targets {
		loaderTmpl, loaderFile, loaderFn, err := loaderFor(target)
		if err != nil {
			return nil, err
		}
		b, err := render(loaderTmpl, nil)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "runtime/" + loaderFile + ".ts", Content: b})

		entry, err := render("index.ts.tmpl", map[string]any{
			"APIName":    api,
			"Funcs":      funcs,
			"Namespace":  opts.Namespace,
			"LoaderFn":   loaderFn,
			"LoaderFile": loaderFile,
		})
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "index." + target + ".ts", Content: entry})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func loaderFor(target string) (tmpl, file, fn string, err error) {
	switch target {
	case "node":
		return "loader.node.ts.tmpl", "loader.node", "loadNode", nil
	case "browser":
		return "loader.browser.ts.tmpl", "loader.browser", "loadBrowser", nil
	default:
		return "", "", "", fmt.Errorf("unknown target %q, want node or browser", target)
	}
}

// reservedJS lists the words that cannot be a variable declaration name in
// JavaScript. A Go function named New maps to new, which is legal as an object
// property but not as `export const new`, and New is far too common a Go name
// to reject outright.
var reservedJS = map[string]bool{
	"await": true, "break": true, "case": true, "catch": true, "class": true,
	"const": true, "continue": true, "debugger": true, "default": true,
	"delete": true, "do": true, "else": true, "enum": true, "export": true,
	"extends": true, "false": true, "finally": true, "for": true,
	"function": true, "if": true, "implements": true, "import": true,
	"in": true, "instanceof": true, "interface": true, "let": true,
	"new": true, "null": true, "package": true, "private": true,
	"protected": true, "public": true, "return": true, "static": true,
	"super": true, "switch": true, "this": true, "throw": true, "true": true,
	"try": true, "typeof": true, "var": true, "void": true, "while": true,
	"with": true, "yield": true,
}

// tsFunc is the per-function view the templates render.
type tsFunc struct {
	JSName     string
	Doc        string
	TSParams   string // "text: string, mode: Strictness"
	TSReturn   string
	BindParams string // "text, mode"
	CallArgs   string // "text, mode" — or "text, rest" for a variadic tail
	// Decode converts a binary-bearing result, in terms of the variable `v`.
	Decode string
	// Reserved marks a name that cannot be a bare named export. The function is
	// still reachable on the client object.
	Reserved bool
	// Key is the name as written in an interface or object literal, quoted when
	// it is a reserved word. Unquoted, `new(): T` would be read as a construct
	// signature rather than a method called new.
	Key string
}

func renderFuncs(mod *scan.Module, cdc *codec) []tsFunc {
	out := make([]tsFunc, 0, len(mod.Funcs))
	for _, f := range mod.Funcs {
		var params, names, args []string
		for i, p := range f.Params {
			last := i == len(f.Params)-1
			if f.Variadic && last {
				// The rest arguments are packed into one array before crossing
				// the boundary, keeping the wire arity fixed.
				params = append(params, "..."+p.JSName+": "+arrayOf(p.TS))
				names = append(names, "..."+p.JSName)
			} else {
				params = append(params, p.JSName+": "+p.TS)
				names = append(names, p.JSName)
			}
			args = append(args, cdc.encode(p.Type, p.JSName))
		}

		fn := tsFunc{
			JSName:     f.JSName,
			Reserved:   reservedJS[f.JSName],
			Key:        memberKey(f.JSName),
			Doc:        f.Doc,
			TSParams:   strings.Join(params, ", "),
			TSReturn:   f.TSReturn(),
			BindParams: strings.Join(names, ", "),
			CallArgs:   strings.Join(args, ", "),
		}

		// A result carrying binary data is converted around the awaited call, so
		// the caller never sees the base64 the wire format uses.
		if len(f.Results) == 1 {
			call := fmt.Sprintf("(await rt.call<any>(%q, [%s]))", f.JSName, fn.CallArgs)
			if decoded := cdc.decode(f.Results[0].Type, call); decoded != call {
				fn.Decode = decoded
			}
		}
		out = append(out, fn)
	}
	return out
}

// memberKey quotes a member name when it is a reserved word.
func memberKey(name string) string {
	if reservedJS[name] {
		return strconv.Quote(name)
	}
	return name
}

// codecImports lists the conversion helpers client.ts references.
func codecImports(cdc *codec, mod *scan.Module, enabled bool) []string {
	if !enabled {
		return nil
	}
	used := map[string]bool{}
	note := func(expr string) {
		for _, name := range identifiers(expr) {
			if strings.HasPrefix(name, "decode") || strings.HasPrefix(name, "encode") ||
				name == "b64ToBytes" || name == "bytesToB64" || name == "mapValues" {
				used[name] = true
			}
		}
	}
	for _, f := range mod.Funcs {
		for _, p := range f.Params {
			note(cdc.encode(p.Type, "x"))
		}
		for _, r := range f.Results {
			note(cdc.decode(r.Type, "x"))
		}
	}
	// mapValues is module-private to codec.ts.
	delete(used, "mapValues")

	out := make([]string, 0, len(used))
	for name := range used {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func arrayOf(ts string) string {
	if strings.Contains(ts, "|") {
		return "(" + ts + ")[]"
	}
	return ts + "[]"
}

// typeImports lists the names client.ts must import from types.ts: every
// declared name that actually appears in a signature. Importing all of them
// unconditionally would trip noUnusedLocals in a strict tsconfig.
func typeImports(mod *scan.Module, funcs []tsFunc) []string {
	declared := map[string]bool{}
	for _, s := range mod.Structs {
		declared[s.Name] = true
	}
	for _, e := range mod.Enums {
		declared[e.Name] = true
	}
	for _, a := range mod.Aliases {
		declared[a.Name] = true
	}
	// Uint8Array is a JavaScript built-in, so it is never imported.
	if mod.UsesISODateTime {
		declared[tsmap.ISODateTime] = true
	}

	used := map[string]bool{}
	for _, f := range funcs {
		for _, ident := range identifiers(f.TSParams + " " + f.TSReturn) {
			if declared[ident] {
				used[ident] = true
			}
		}
	}

	out := make([]string, 0, len(used))
	for name := range used {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

var identRe = regexp.MustCompile(`[A-Za-z_$][A-Za-z0-9_$]*`)

func identifiers(s string) []string { return identRe.FindAllString(s, -1) }

// apiName turns a Go package name into the exported interface name: urls -> Urls.
func apiName(pkg string) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, pkg)
	if cleaned == "" {
		return "API"
	}
	r := []rune(cleaned)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func render(name string, data any) ([]byte, error) {
	t, err := template.New(name).Funcs(template.FuncMap{
		"join": strings.Join,
		"doc":  tsDoc,
	}).ParseFS(tmplFS, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering %s: %w", name, err)
	}
	return normalize(buf.Bytes()), nil
}

// tsDoc renders a Go doc comment as a TSDoc block at the given indent, or
// nothing at all when there is no comment.
func tsDoc(doc, indent string) string {
	doc = strings.TrimSpace(doc)
	if doc == "" {
		return ""
	}
	lines := strings.Split(doc, "\n")
	if len(lines) == 1 {
		return indent + "/** " + lines[0] + " */\n"
	}
	var b strings.Builder
	b.WriteString(indent + "/**\n")
	for _, l := range lines {
		b.WriteString(strings.TrimRight(indent+" * "+l, " ") + "\n")
	}
	b.WriteString(indent + " */\n")
	return b.String()
}

var blankRuns = regexp.MustCompile(`\n{3,}`)

// normalize collapses the blank-line runs that conditional template blocks
// leave behind, so output is stable and diffable.
func normalize(b []byte) []byte {
	b = blankRuns.ReplaceAll(b, []byte("\n\n"))
	return append(bytes.TrimRight(b, "\n"), '\n')
}
