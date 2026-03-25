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

// Generate renders every scaffold file.
func Generate(cfg *config.Config, mod *scan.Module) ([]File, error) {
	pkgJSON, err := packageJSON(cfg)
	if err != nil {
		return nil, err
	}

	files := []File{
		{Path: "package.json", Content: pkgJSON},
	}

	for _, s := range []struct{ tmpl, path string }{
		{"tsconfig.json.tmpl", "tsconfig.json"},
		{"tsconfig.test.json.tmpl", "tsconfig.test.json"},
		{"copy-assets.mjs.tmpl", "scripts/copy-assets.mjs"},
		{"gitignore.tmpl", ".gitignore"},
	} {
		b, err := render(s.tmpl, nil)
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: s.path, Content: b})
	}

	readme, err := render("README.md.tmpl", readmeData(cfg, mod))
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

func packageJSON(cfg *config.Config) ([]byte, error) {
	node := &conditions{Types: "./dist/index.node.d.ts", Default: "./dist/index.node.js"}
	browser := &conditions{Types: "./dist/index.browser.d.ts", Default: "./dist/index.browser.js"}

	root := &rootExport{}
	if hasTarget(cfg.Targets, "node") {
		root.Node = node
	}
	if hasTarget(cfg.Targets, "browser") {
		root.Browser = browser
	}
	// The default condition must always resolve to something. Prefer the
	// browser build when present, since bundlers that ignore "browser" still
	// land somewhere that works.
	switch {
	case root.Browser != nil:
		root.Default = browser
	case root.Node != nil:
		root.Default = node
	default:
		return nil, fmt.Errorf("no targets configured")
	}

	m := manifest{
		Name:        cfg.NPM.Name,
		Version:     cfg.NPM.Version,
		Description: cfg.NPM.Description,
		License:     cfg.NPM.License,
		Author:      cfg.NPM.Author,
		Type:        "module",
		Exports: map[string]any{
			".":              root,
			"./main.wasm":    "./dist/main.wasm",
			"./package.json": "./package.json",
		},
		// Publishing dist/ alone is what frees consumers from needing Go: the
		// compiled module and the runtime bridge are both inside it.
		Files:       []string{"dist"},
		SideEffects: false,
		Engines:     map[string]string{"node": ">=20"},
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

func readmeData(cfg *config.Config, mod *scan.Module) map[string]any {
	type fnView struct {
		JSName   string
		TSParams string
		TSReturn string
		Doc      string
	}
	var funcs []fnView
	for _, f := range mod.Funcs {
		var params []string
		for _, p := range f.Params {
			params = append(params, p.JSName+": "+p.TS)
		}
		funcs = append(funcs, fnView{
			JSName:   f.JSName,
			TSParams: strings.Join(params, ", "),
			TSReturn: f.TSReturn(),
			Doc:      firstSentence(f.Doc),
		})
	}

	first, args := "", ""
	if len(mod.Funcs) > 0 {
		first = mod.Funcs[0].JSName
		args = exampleArgs(mod.Funcs[0])
	}

	return map[string]any{
		"Name":        cfg.NPM.Name,
		"Description": cfg.NPM.Description,
		"PkgPath":     mod.PkgPath,
		"Funcs":       funcs,
		"FirstFunc":   first,
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
