package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/paulgrammer/gowasm/internal/config"
	"github.com/paulgrammer/gowasm/internal/pkgmgr"
	"github.com/paulgrammer/gowasm/internal/prompt"
	"github.com/paulgrammer/gowasm/internal/runner"
	"gopkg.in/yaml.v3"
)

// initCmd walks the user through creating gowasm.yaml, the way npm init walks
// through package.json.
func initCmd(dir string, yes bool, out io.Writer) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	p := prompt.New(os.Stdin, out, yes, isTerminal(os.Stdin))

	cfg := defaults(abs)
	existing := filepath.Join(abs, config.FileName)
	if _, err := os.Stat(existing); err == nil {
		// Re-running init should refine the config, not reset it.
		if prev, err := config.Load(abs); err == nil {
			merge(cfg, prev)
			p.Printf("Updating the existing %s.\n\n", config.FileName)
		}
	} else {
		p.Printf("This utility walks you through creating a %s file.\n", config.FileName)
		p.Printf("Press Enter to accept the default shown in parentheses.\n\n")
	}

	if err := ensureGoModule(abs, p, out); err != nil {
		return err
	}

	if cfg.NPM.Name, err = p.AskValid("package name:", cfg.NPM.Name, config.ValidateNPMName); err != nil {
		return err
	}
	if cfg.NPM.Version, err = p.Ask("version:", cfg.NPM.Version); err != nil {
		return err
	}
	if cfg.NPM.Description, err = p.Ask("description:", cfg.NPM.Description); err != nil {
		return err
	}
	if cfg.Package, err = p.AskValid("go package:", cfg.Package, validatePackageDir(abs)); err != nil {
		return err
	}
	// Said here rather than after writing, so the offer is visible before the
	// confirmation rather than as a surprise afterwards.
	pkgDir := cfg.Package
	if !filepath.IsAbs(pkgDir) {
		pkgDir = filepath.Join(abs, pkgDir)
	}
	if !hasGoFiles(pkgDir) {
		p.Printf("  no Go files there yet, so a starter package will be written\n")
	}
	if cfg.Out, err = p.Ask("output directory:", cfg.Out); err != nil {
		return err
	}
	if cfg.NPM.License, err = p.Ask("license:", cfg.NPM.License); err != nil {
		return err
	}
	if cfg.NPM.Author, err = p.Ask("author:", cfg.NPM.Author); err != nil {
		return err
	}
	if cfg.NPM.Repository, err = p.Ask("repository:", cfg.NPM.Repository); err != nil {
		return err
	}

	targets, err := p.AskValid("targets:", strings.Join(cfg.Targets, ","), validateTargets)
	if err != nil {
		return err
	}
	cfg.Targets = splitTargets(targets)

	if cfg.PackageManager, err = p.AskValid("package manager:", cfg.PackageManager, validateManager); err != nil {
		return err
	}

	if err := cfg.Validate(); err != nil {
		return err
	}

	preview, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Printf("\nAbout to write to %s:\n\n%s\n", existing, indent(string(preview), "  "))

	ok, err := p.Confirm("Is this OK?", true)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(out, "aborted; nothing was written")
		return nil
	}

	path, err := cfg.Write(abs)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nwrote %s\n", rel(abs, path))

	// A new project has a module but no code, so gowasm build would fail on a
	// package with nothing in it. Give it something that works.
	wrote, err := writeStarter(abs, cfg.Package, modulePath(abs))
	if err != nil {
		return err
	}
	if wrote {
		fmt.Fprintf(out, "wrote a starter package in %s\n", cfg.Package)
	}

	fmt.Fprintf(out, "\nNext:\n  gowasm build\n  gowasm test\n")
	return nil
}

// defaults infers everything it can from the surrounding project rather than
// inventing values.
func defaults(dir string) *config.Config {
	cfg := &config.Config{
		Out:     "./node",
		Package: ".",
		Targets: []string{"node", "browser"},
		// Detected from a lockfile if the project has one, so an existing
		// choice is honoured rather than overridden.
		PackageManager: string(pkgmgr.Detect(dir)),
		Int64:          "number",
		NPM:            config.NPM{Version: "0.1.0", License: "MIT"},
		Dir:            dir,
	}

	if mod := modulePath(dir); mod != "" {
		cfg.NPM.Name = strings.ToLower(filepath.Base(mod))
	} else {
		cfg.NPM.Name = strings.ToLower(filepath.Base(dir))
	}

	if name, _ := runner.Output(dir, "git", "config", "user.name"); name != "" {
		if email, _ := runner.Output(dir, "git", "config", "user.email"); email != "" {
			cfg.NPM.Author = fmt.Sprintf("%s <%s>", name, email)
		} else {
			cfg.NPM.Author = name
		}
	}

	if origin, _ := runner.Output(dir, "git", "remote", "get-url", "origin"); origin != "" {
		cfg.NPM.Repository = normalizeOrigin(origin)
	}

	// When exactly one subdirectory holds Go files, it is almost certainly the
	// package to expose.
	if pkgs := goPackageDirs(dir); len(pkgs) == 1 {
		cfg.Package = pkgs[0]
	}
	return cfg
}

func merge(cfg, prev *config.Config) {
	if prev.Package != "" {
		cfg.Package = prev.Package
	}
	if prev.Out != "" {
		cfg.Out = prev.Out
	}
	if len(prev.Targets) > 0 {
		cfg.Targets = prev.Targets
	}
	if prev.Int64 != "" {
		cfg.Int64 = prev.Int64
	}
	if prev.PackageManager != "" {
		cfg.PackageManager = prev.PackageManager
	}
	if prev.NPM.Name != "" {
		cfg.NPM.Name = prev.NPM.Name
	}
	if prev.NPM.Version != "" {
		cfg.NPM.Version = prev.NPM.Version
	}
	if prev.NPM.Description != "" {
		cfg.NPM.Description = prev.NPM.Description
	}
	if prev.NPM.License != "" {
		cfg.NPM.License = prev.NPM.License
	}
	if prev.NPM.Author != "" {
		cfg.NPM.Author = prev.NPM.Author
	}
	if prev.NPM.Repository != "" {
		cfg.NPM.Repository = prev.NPM.Repository
	}
}

// ensureGoModule offers to create a module when there is none, so init works in
// an empty directory as well as an existing project.
func ensureGoModule(dir string, p *prompt.Prompter, out io.Writer) error {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
		return nil
	}
	p.Printf("No go.mod here yet.\n")
	name, err := p.Ask("go module path:", "example.com/"+strings.ToLower(filepath.Base(dir)))
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("a Go module is required; run 'go mod init <path>' and try again")
	}
	r := runner.New(false)
	if err := r.Run(dir, nil, "go", "mod", "init", name); err != nil {
		return err
	}
	fmt.Fprintln(out)
	return nil
}

// validatePackageDir accepts a directory that does not exist yet, or exists
// with no Go files in it.
//
// Rejecting those was a dead end: in a new project the module has just been
// created and there is no Go source anywhere, so every possible answer failed
// validation and the question asked itself forever. A directory without Go
// files is now a thing to fill in rather than a mistake.
func validatePackageDir(base string) func(string) error {
	return func(p string) error {
		if p == "" {
			return fmt.Errorf("required; use . for the current directory")
		}
		target := p
		if !filepath.IsAbs(target) {
			target = filepath.Join(base, p)
		}
		info, err := os.Stat(target)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", p)
		}
		return nil
	}
}

// starter is the package written into an empty project, so that gowasm build
// works immediately rather than failing on a directory with nothing in it.
//
// It is deliberately small but not trivial: a struct, an enum from constants,
// an error path and an Example function, so the first build demonstrates the
// type mapping and records a behavioural fixture.
const starter = `// Package %[1]s is where your Go code goes.
//
// Every exported function here becomes a TypeScript function in the generated
// package. There is nothing to annotate: write ordinary Go, run gowasm build,
// and call it from JavaScript.
package %[1]s

import (
	"fmt"
	"strings"
)

// Tone selects how enthusiastic a greeting is. Constants of a named type
// become a TypeScript literal union, so a typo is a compile error.
type Tone string

const (
	Plain Tone = "plain"
	Loud  Tone = "loud"
)

// Greeting is what Greet returns. Struct fields use their json tags for names.
type Greeting struct {
	Text string %[2]sjson:"text"%[2]s
	// Length is in characters, not bytes.
	Length int %[2]sjson:"length"%[2]s
}

// Greet builds a greeting. The error becomes a rejected promise.
func Greet(name string, tone Tone) (Greeting, error) {
	if strings.TrimSpace(name) == "" {
		return Greeting{}, fmt.Errorf("a greeting needs a name")
	}

	text := "Hello, " + name + "."
	if tone == Loud {
		text = strings.ToUpper("Hello, " + name + "!")
	}
	return Greeting{Text: text, Length: len([]rune(text))}, nil
}
`

const starterTest = `package %[1]s_test

import (
	"fmt"

	"%[2]s"
)

// Calls in an Example function with literal arguments are run by gowasm and
// replayed as TypeScript tests, so this expectation cannot drift from the code.

func ExampleGreet() {
	g, _ := %[1]s.Greet("world", %[1]s.Plain)
	fmt.Println(g.Text, g.Length)
	// Output: Hello, world. 13
}

func ExampleGreet_loud() {
	g, _ := %[1]s.Greet("world", %[1]s.Loud)
	fmt.Println(g.Text)
	// Output: HELLO, WORLD!
}

func ExampleGreet_noName() {
	_, err := %[1]s.Greet("  ", %[1]s.Plain)
	fmt.Println(err)
	// Output: a greeting needs a name
}
`

// writeStarter creates a working package at dir when it holds no Go files.
// It reports whether anything was written.
func writeStarter(base, pkgPath, modulePath string) (bool, error) {
	target := pkgPath
	if !filepath.IsAbs(target) {
		target = filepath.Join(base, pkgPath)
	}
	if hasGoFiles(target) {
		return false, nil
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return false, err
	}

	// At the module root the directory name is incidental -- it can be a
	// checkout path, or a numbered temporary directory -- while the module's
	// last segment is the name the project actually goes by.
	source := filepath.Base(target)
	if p := strings.Trim(pkgPath, "./"); p == "" {
		source = filepath.Base(modulePath)
	}
	name := goPackageName(source)
	importPath := modulePath
	if rel := strings.TrimPrefix(strings.TrimPrefix(pkgPath, "."), "/"); rel != "" {
		importPath = modulePath + "/" + filepath.ToSlash(rel)
	}

	// Go pairs foo.go with foo_test.go. example_test.go is a real convention
	// too, used in the standard library for files holding nothing but Example
	// functions, but for a one-file starter the matching name reads better and
	// is what people expect to find.
	files := map[string]string{
		name + ".go":      fmt.Sprintf(starter, name, "`"),
		name + "_test.go": fmt.Sprintf(starterTest, name, importPath),
	}
	for filename, body := range files {
		path := filepath.Join(target, filename)
		if _, err := os.Stat(path); err == nil {
			continue // never overwrite something already there
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return false, fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return true, nil
}

// goPackageName reduces a name to something legal as a Go package identifier.
func goPackageName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" || (out[0] >= '0' && out[0] <= '9') {
		out = "app" + out
	}
	return out
}

func validateManager(s string) error {
	m, err := pkgmgr.Parse(s)
	if err != nil {
		return err
	}
	if !m.Available() {
		// Not fatal: someone may be configuring a project for a machine other
		// than this one. Worth saying, though, since the next build will fail.
		return fmt.Errorf("%s is not installed here; install it or choose another", m)
	}
	return nil
}

func validateTargets(s string) error {
	parts := splitTargets(s)
	if len(parts) == 0 {
		return fmt.Errorf("pick at least one of node, browser")
	}
	for _, t := range parts {
		if t != "node" && t != "browser" {
			return fmt.Errorf("unknown target %q; use node, browser, or both", t)
		}
	}
	return nil
}

func splitTargets(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- project inspection ---

func modulePath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

func goPackageDirs(dir string) []string {
	var out []string
	if hasGoFiles(dir) {
		out = append(out, ".")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
			name == "node_modules" || name == "vendor" || name == "testdata" {
			continue
		}
		if hasGoFiles(filepath.Join(dir, name)) {
			out = append(out, "./"+name)
		}
	}
	return out
}

func hasGoFiles(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			return true
		}
	}
	return false
}

func normalizeOrigin(url string) string {
	url = strings.TrimSuffix(strings.TrimSpace(url), ".git")
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	if rest, ok := strings.CutPrefix(url, "git@"); ok {
		url = strings.Replace(rest, ":", "/", 1)
	}
	return url
}

func indent(s, with string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = with + l
	}
	return strings.Join(lines, "\n")
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
