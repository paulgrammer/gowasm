// Package scaffold emits the non-TypeScript files of the generated npm
// package: manifest, tsconfigs, build script, README, license and the browser
// smoke page.
package scaffold

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/paulgrammer/gowasm/internal/config"
	"github.com/paulgrammer/gowasm/internal/scan"
)

//go:embed templates/*.tmpl
var tmplFS embed.FS

// File is one emitted file, relative to the package root.
type File struct {
	Path    string
	Content []byte
}

// hasClasses reports whether any package exposes a type with identity. Only
// those need the disposal protocol, so a package without one keeps exactly the
// tsconfig and the engines range it had.
func hasClasses(b *scan.Bundle) bool {
	for _, mod := range b.Modules {
		if len(mod.Classes) > 0 {
			return true
		}
	}
	return false
}

// Generate renders every scaffold file.
func Generate(cfg *config.Config, b *scan.Bundle) ([]File, error) {
	pkgJSON, err := packageJSON(cfg, b)
	if err != nil {
		return nil, err
	}

	files := []File{
		{Path: "package.json", Content: pkgJSON},
	}

	// Symbol.asyncDispose is declared only in TypeScript's esnext.disposable
	// library, and `await using` on a generated class needs it.
	tsconfigData := map[string]any{"Classes": hasClasses(b)}
	for _, s := range []struct {
		tmpl, path string
		data       any
	}{
		{"tsconfig.json.tmpl", "tsconfig.json", tsconfigData},
		{"tsconfig.test.json.tmpl", "tsconfig.test.json", nil},
		{"copy-assets.mjs.tmpl", "scripts/copy-assets.mjs", nil},
		{"gitignore.tmpl", ".gitignore", nil},
	} {
		content, err := render(s.tmpl, s.data)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: s.path, Content: content})
	}

	readme, err := render("README.md.tmpl", readmeData(cfg, b))
	if err != nil {
		return nil, err
	}
	files = append(files, File{Path: "README.md", Content: readme})

	// Only MIT is generated in full; any other license is the author's to supply.
	if strings.EqualFold(cfg.NPM.License, "MIT") {
		author := cfg.NPM.Author
		if author == "" {
			author = "the authors"
		}
		lic, err := render("LICENSE.tmpl", map[string]any{
			"Year":   time.Now().Year(),
			"Author": author,
		})
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "LICENSE", Content: lic})
	}

	if hasTarget(cfg.Targets, "browser") {
		html, err := render("index.html.tmpl", map[string]any{"Name": cfg.NPM.Name})
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: "example/index.html", Content: html})
	}

	return files, nil
}

// --- package.json ---

type conditions struct {
	Types   string `json:"types"`
	Default string `json:"default"`
}

// rootExport orders conditions most-specific first, as the resolution
// algorithm picks the first match.
type rootExport struct {
	Node    *conditions `json:"node,omitempty"`
	Browser *conditions `json:"browser,omitempty"`
	Default *conditions `json:"default"`
}

type repository struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type manifest struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description,omitempty"`
	License     string            `json:"license,omitempty"`
	Author      string            `json:"author,omitempty"`
	Repository  *repository       `json:"repository,omitempty"`
	Type        string            `json:"type"`
	Exports     map[string]any    `json:"exports"`
	Files       []string          `json:"files"`
	SideEffects bool              `json:"sideEffects"`
	Engines     map[string]string `json:"engines"`
	Scripts     map[string]string `json:"scripts"`
	DevDeps     map[string]string `json:"devDependencies"`
}

// entryExport builds the conditions for one entry point, given the base name
// its compiled files carry: "index" for the package root, or the namespace for
// a subpath.
func entryExport(cfg *config.Config, base string) (*rootExport, error) {
	node := &conditions{
		Types:   "./dist/" + base + ".node.d.ts",
		Default: "./dist/" + base + ".node.js",
	}
	browser := &conditions{
		Types:   "./dist/" + base + ".browser.d.ts",
		Default: "./dist/" + base + ".browser.js",
	}

	e := &rootExport{}
	if hasTarget(cfg.Targets, "node") {
		e.Node = node
	}
	if hasTarget(cfg.Targets, "browser") {
		e.Browser = browser
	}
	// The default condition must always resolve to something. Prefer the
	// browser build when present, since bundlers that ignore "browser" still
	// land somewhere that works.
	switch {
	case e.Browser != nil:
		e.Default = browser
	case e.Node != nil:
		e.Default = node
	default:
		return nil, fmt.Errorf("no targets configured")
	}
	return e, nil
}

func packageJSON(cfg *config.Config, b *scan.Bundle) ([]byte, error) {
	// A package with classes needs Symbol.asyncDispose, which arrives in Node
	// 20.4. Below it `await using` would be a silently useless member, and the
	// alternative -- a library mutating the global Symbol -- would contradict
	// "sideEffects": false. A package without classes keeps the wider range.
	node := ">=20"
	if hasClasses(b) {
		node = ">=20.4.0"
	}
	root, err := entryExport(cfg, "index")
	if err != nil {
		return nil, err
	}

	exports := map[string]any{
		".":              root,
		"./main.wasm":    "./dist/main.wasm",
		"./package.json": "./package.json",
	}
	// Each namespace is also a subpath, so a consumer who wants one package can
	// import it alone and leave the rest out of their bundle. The same module
	// backs both forms, so there is nothing to keep in sync.
	for _, mod := range b.Modules {
		if mod.Namespace == "" {
			continue
		}
		e, err := entryExport(cfg, mod.Namespace)
		if err != nil {
			return nil, err
		}
		exports["./"+mod.Namespace] = e
	}

	m := manifest{
		Name:        cfg.NPM.Name,
		Version:     cfg.NPM.Version,
		Description: cfg.NPM.Description,
		License:     cfg.NPM.License,
		Author:      cfg.NPM.Author,
		Type:        "module",
		Exports:     exports,
		// Publishing dist/ alone is what frees consumers from needing Go: the
		// compiled module and the runtime bridge are both inside it.
		Files:       []string{"dist"},
		SideEffects: false,
		Engines:     map[string]string{"node": node},
		Scripts: map[string]string{
			"build":     "tsc && node scripts/copy-assets.mjs",
			"typecheck": "tsc -p tsconfig.test.json",
			"test":      "npm run typecheck && node --test \"test/**/*.test.ts\"",
		},
		DevDeps: map[string]string{
			"typescript":  "^5.9.0",
			"@types/node": "^24.0.0",
		},
	}
	if cfg.NPM.Repository != "" {
		m.Repository = &repository{Type: "git", URL: normalizeRepo(cfg.NPM.Repository)}
	}

	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func normalizeRepo(s string) string {
	switch {
	case strings.HasPrefix(s, "git+"), strings.HasPrefix(s, "https://"), strings.HasPrefix(s, "http://"):
		return s
	case strings.HasPrefix(s, "git@"):
		s = strings.Replace(s, ":", "/", 1)
		return "git+https://" + strings.TrimPrefix(s, "git@")
	default:
		return "git+https://" + s + ".git"
	}
}

// --- README ---

type fnView struct {
	JSName   string
	TSParams string
	TSReturn string
	Doc      string
}

// nsView is one package's section of the README.
type nsView struct {
	NS      string
	PkgPath string
	Funcs   []fnView
}

func readmeData(cfg *config.Config, b *scan.Bundle) map[string]any {
	views := make([]nsView, 0, len(b.Modules))
	paths := make([]string, 0, len(b.Modules))
	for _, mod := range b.Modules {
		v := nsView{NS: mod.Namespace, PkgPath: mod.PkgPath}
		for _, f := range mod.Funcs {
			var params []string
			for _, p := range f.Params {
				params = append(params, p.JSName+": "+p.TS)
			}
			v.Funcs = append(v.Funcs, fnView{
				JSName:   f.JSName,
				TSParams: strings.Join(params, ", "),
				TSReturn: f.TSReturn(),
				Doc:      firstSentence(f.Doc),
			})
		}
		views = append(views, v)
		paths = append(paths, mod.PkgPath)
	}

	// The opening snippet uses the first function of the first package, since a
	// snippet nobody can run is worse than none.
	first, args, ns := "", "", ""
	if mods := b.Modules; len(mods) > 0 && len(mods[0].Funcs) > 0 {
		first = mods[0].Funcs[0].JSName
		args = exampleArgs(mods[0].Funcs[0])
		ns = mods[0].Namespace
	}
	// Namespaced exports are called through the namespace: lib.extract(…).
	call := first
	imported := first
	if ns != "" {
		call = ns + "." + first
		imported = ns
	}

	return map[string]any{
		"Name":        cfg.NPM.Name,
		"Description": cfg.NPM.Description,
		"PkgPath":     strings.Join(paths, "`, `"),
		"Multi":       b.Multi(),
		"Namespaces":  views,
		"FirstFunc":   first,
		"FirstNS":     ns,
		"FirstCall":   call,
		"FirstImport": imported,
		"FirstArgs":   args,
	}
}

// exampleArgs invents a plausible call for the README, so the snippet is
// copy-pasteable rather than a placeholder.
func exampleArgs(f scan.Func) string {
	var out []string
	for _, p := range f.Params {
		out = append(out, sampleValue(p.TS))
	}
	return strings.Join(out, ", ")
}

func sampleValue(ts string) string {
	switch {
	case ts == "string" || ts == "ISODateTime":
		return `"…"`
	case ts == "Uint8Array":
		return "new Uint8Array([1, 2, 3])"
	case ts == "number":
		return "0"
	case ts == "boolean":
		return "true"
	case strings.HasPrefix(ts, `"`):
		// A literal union: use its first member.
		if i := strings.Index(ts, `"`); i >= 0 {
			if j := strings.Index(ts[i+1:], `"`); j >= 0 {
				return ts[i : i+j+2]
			}
		}
		return `"…"`
	case strings.HasSuffix(ts, "[]"):
		return "[]"
	case strings.HasPrefix(ts, "Record<"):
		return "{}"
	default:
		return "…"
	}
}

func firstSentence(doc string) string {
	doc = strings.TrimSpace(strings.ReplaceAll(doc, "\n", " "))
	if doc == "" {
		return ""
	}
	if i := strings.Index(doc, ". "); i >= 0 {
		return doc[:i+1]
	}
	return doc
}

// --- helpers ---

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
