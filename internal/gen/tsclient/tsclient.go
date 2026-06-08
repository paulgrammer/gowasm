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
	// Package is the published npm name, used only in the doc comments that
	// show how to import each namespace.
	Package string
	// Targets selects which entry points to emit: "node", "browser", or both.
	Targets []string
}

// ReservedNames lists the exported functions whose JavaScript name cannot be a
// bare named export, so callers can be told rather than left to wonder.
func ReservedNames(b *scan.Bundle) []string {
	var out []string
	for _, mod := range b.Modules {
		for _, f := range mod.Funcs {
			if tsmap.ReservedJS[f.JSName] {
				out = append(out, f.GoName+" -> "+mod.Wire(f))
			}
		}
	}
	sort.Strings(out)
	return out
}

// nsView is one package's place in the generated package, shared by the entry
// point and the namespace modules.
type nsView struct {
	NS      string
	PkgName string
	APIName string
	First   string
	Funcs   []tsFunc
	Classes []string
}

// Generate renders every TypeScript file for the bundle.
//
// One package produces the flat layout it always has: generated/types.ts,
// generated/client.ts and an entry point exporting the functions directly.
// Several produce one directory and one namespace module each, re-exported
// from the entry point and reachable as subpaths.
func Generate(b *scan.Bundle, opts Options) ([]File, error) {
	files := []File{}

	// Static runtime pieces: identical for every package.
	for _, s := range []struct{ tmpl, path string }{
		{"core.ts.tmpl", "runtime/core.ts"},
		{"wasm_exec.d.ts.tmpl", "vendor/wasm_exec.d.ts"},
	} {
		content, err := render(s.tmpl, nil)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: s.path, Content: content})
	}

	multi := b.Multi()
	views := make([]nsView, 0, len(b.Modules))

	for _, mod := range b.Modules {
		// The flat layout keeps generated/ directly; a namespaced one gives each
		// package its own directory beneath it.
		dir, coreDir := "generated", ".."
		if multi {
			dir, coreDir = "generated/"+mod.Namespace, "../.."
		}

		types, err := render("types.ts.tmpl", mod)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: dir + "/types.ts", Content: types})

		cs := newClassSet(mod)
		cdc := newCodec(mod, cs)
		useCodec := cdc.Needed(mod)
		funcs := renderFuncs(mod, cdc, cs)
		if useCodec {
			codecViews, usesMapValues := buildCodecViews(cdc, mod)
			content, err := render("codec.ts.tmpl", map[string]any{
				"Structs":       codecViews,
				"UsesMapValues": usesMapValues,
			})
			if err != nil {
				return nil, err
			}
			files = append(files, File{Path: dir + "/codec.ts", Content: content})
		}

		api := apiName(mod)
		client, err := render("client.ts.tmpl", map[string]any{
			"PkgName":      mod.PkgName,
			"APIName":      api,
			"CoreDir":      coreDir + "/runtime",
			"Funcs":        funcs,
			"TypeImports":  typeImports(mod, funcs),
			"ClassImports": usedClasses(mod, funcs),
			"CodecImports": codecImports(cdc, mod, useCodec),
		})
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: dir + "/client.ts", Content: client})

		// A class is a value, not a shape, so it lives in its own module rather
		// than in the type-only types.ts.
		if len(mod.Classes) > 0 {
			views := buildClasses(mod, cdc, cs)
			content, err := render("classes.ts.tmpl", map[string]any{
				"Classes":      views,
				"CoreDir":      coreDir + "/runtime",
				"TypeImports":  classTypeImports(mod, views),
				"CodecImports": classCodecImports(views),
			})
			if err != nil {
				return nil, err
			}
			files = append(files, File{Path: dir + "/classes.ts", Content: content})
		}

		v := nsView{NS: mod.Namespace, PkgName: mod.PkgName, APIName: api, Funcs: funcs, Classes: classNames(mod)}
		if len(funcs) > 0 {
			v.First = funcs[0].JSName
		}
		views = append(views, v)
	}

	for _, target := range opts.Targets {
		loaderTmpl, loaderFile, loaderFn, err := loaderFor(target)
		if err != nil {
			return nil, err
		}
		content, err := render(loaderTmpl, nil)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "runtime/" + loaderFile + ".ts", Content: content})

		if !multi {
			entry, err := render("index.ts.tmpl", map[string]any{
				"APIName":    views[0].APIName,
				"Funcs":      views[0].Funcs,
				"Classes":    classNames(b.Modules[0]),
				"Namespace":  opts.Namespace,
				"LoaderFn":   loaderFn,
				"LoaderFile": loaderFile,
			})
			if err != nil {
				return nil, err
			}
			files = append(files, File{Path: "index." + target + ".ts", Content: entry})
			continue
		}

		// The shared instance moves into its own module once there are several
		// namespaces, because all of them have to reach the same one.
		instance, err := render("instance.ts.tmpl", map[string]any{
			"Namespace":  opts.Namespace,
			"LoaderFn":   loaderFn,
			"LoaderFile": loaderFile,
		})
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "runtime/instance." + target + ".ts", Content: instance})

		for _, v := range views {
			nsFile, err := render("namespace.ts.tmpl", map[string]any{
				"NS":      v.NS,
				"PkgName": v.PkgName,
				"APIName": v.APIName,
				"First":   v.First,
				"Funcs":   v.Funcs,
				"Classes": v.Classes,
				"Package": opts.Package,
				"Target":  target,
			})
			if err != nil {
				return nil, err
			}
			files = append(files, File{Path: v.NS + "." + target + ".ts", Content: nsFile})
		}

		hasClasses := false
		for _, mod := range b.Modules {
			if len(mod.Classes) > 0 {
				hasClasses = true
			}
		}
		entry, err := render("index.multi.ts.tmpl", map[string]any{
			"Namespaces": views,
			"HasClasses": hasClasses,
			"First":      views[0],
			"Package":    opts.Package,
			"Target":     target,
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

// tsFunc is the per-function view the templates render.
type tsFunc struct {
	JSName string
	// Wire is the name this function is registered under across the boundary,
	// namespaced by package when the project exposes more than one.
	Wire       string
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

func renderFuncs(mod *scan.Module, cdc *codec, cs classSet) []tsFunc {
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
			if name, isClass := cs.of(p.Type); isClass {
				args = append(args, fmt.Sprintf("%s.__unwrap(%s, rt, %q)", name, p.JSName, f.JSName))
				continue
			}
			args = append(args, cdc.encode(p.Type, p.JSName))
		}

		fn := tsFunc{
			JSName:     f.JSName,
			Wire:       mod.Wire(f),
			Reserved:   tsmap.ReservedJS[f.JSName],
			Key:        memberKey(f.JSName),
			Doc:        f.Doc,
			TSParams:   strings.Join(params, ", "),
			TSReturn:   f.TSReturn(),
			BindParams: strings.Join(names, ", "),
			CallArgs:   strings.Join(args, ", "),
		}

		// A result carrying binary data is converted around the awaited call, so
		// the caller never sees the base64 the wire format uses. A returned
		// handle is wrapped into its class in the same place.
		if len(f.Results) == 1 {
			if name, isClass := cs.of(f.Results[0].Type); isClass {
				fn.Decode = fmt.Sprintf("%s.__wrap(rt, await rt.call<number>(%q, [%s]))",
					name, fn.Wire, fn.CallArgs)
			} else {
				call := fmt.Sprintf("(await rt.call<any>(%q, [%s]))", fn.Wire, fn.CallArgs)
				if decoded := cdc.decode(f.Results[0].Type, call); decoded != call {
					fn.Decode = decoded
				}
			}
		}
		out = append(out, fn)
	}
	return out
}

// memberKey quotes a member name when it is a reserved word.
func memberKey(name string) string {
	if tsmap.ReservedJS[name] {
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

// usedClasses lists the classes client.ts references. They are value imports,
// not type imports: bind() constructs them.
func usedClasses(mod *scan.Module, funcs []tsFunc) []string {
	declared := map[string]bool{}
	for _, c := range mod.Classes {
		declared[c.Name] = true
	}
	used := map[string]bool{}
	for _, f := range funcs {
		for _, ident := range identifiers(f.TSParams + " " + f.TSReturn + " " + f.CallArgs + " " + f.Decode) {
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

// classTypeImports lists the declared type names classes.ts mentions.
func classTypeImports(mod *scan.Module, views []classView) []string {
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
	if mod.UsesISODateTime {
		declared[tsmap.ISODateTime] = true
	}

	used := map[string]bool{}
	note := func(s string) {
		for _, ident := range identifiers(s) {
			if declared[ident] {
				used[ident] = true
			}
		}
	}
	for _, v := range views {
		for _, f := range v.Fields {
			note(f.TS)
		}
		for _, m := range v.Methods {
			note(m.TSParams + " " + m.TSReturn)
		}
	}
	out := make([]string, 0, len(used))
	for name := range used {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// classCodecImports lists the conversion helpers classes.ts references.
func classCodecImports(views []classView) []string {
	used := map[string]bool{}
	note := func(expr string) {
		for _, name := range identifiers(expr) {
			if strings.HasPrefix(name, "decode") || strings.HasPrefix(name, "encode") ||
				name == "b64ToBytes" || name == "bytesToB64" {
				used[name] = true
			}
		}
	}
	for _, v := range views {
		for _, f := range v.Fields {
			note(f.GetExpr)
			note(f.SetArg)
		}
		for _, m := range v.Methods {
			note(m.CallExpr)
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
//
// The package's own types are exported from the same module, so a package named
// store that also declares a type Store would declare that name twice and fail
// to compile. The generated interface is the one that yields, since renaming it
// costs nothing while renaming the user's type is not ours to do.
func apiName(mod *scan.Module) string {
	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, mod.PkgName)
	base := "API"
	if cleaned != "" {
		r := []rune(cleaned)
		r[0] = unicode.ToUpper(r[0])
		base = string(r)
	}

	taken := map[string]bool{}
	for _, s := range mod.Structs {
		taken[s.Name] = true
	}
	// A class is exported from the same entry point, so the interface has to
	// yield to it too.
	for _, c := range mod.Classes {
		taken[c.Name] = true
	}
	for _, e := range mod.Enums {
		taken[e.Name] = true
	}
	for _, a := range mod.Aliases {
		taken[a.Name] = true
	}
	if mod.UsesISODateTime {
		taken[tsmap.ISODateTime] = true
	}

	if !taken[base] {
		return base
	}
	if !taken[base+"API"] {
		return base + "API"
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%sAPI%d", base, n)
		if !taken[candidate] {
			return candidate
		}
	}
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
