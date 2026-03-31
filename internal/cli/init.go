package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/paulgrammer/gowasm/internal/config"
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
	fmt.Fprintf(out, "\nwrote %s\n\nNext:\n  gowasm build\n", rel(abs, path))
	return nil
}

// defaults infers everything it can from the surrounding project rather than
// inventing values.
func defaults(dir string) *config.Config {
	cfg := &config.Config{
		Out:     "./node",
		Package: ".",
		Targets: []string{"node", "browser"},
		Int64:   "number",
		NPM:     config.NPM{Version: "0.1.0", License: "MIT"},
		Dir:     dir,
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
		if err != nil {
			return fmt.Errorf("%s does not exist", p)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", p)
		}
		if !hasGoFiles(target) {
			return fmt.Errorf("%s contains no .go files", p)
		}
		return nil
	}
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
