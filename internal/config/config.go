// Package config loads and validates gowasm.yaml.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/paulgrammer/gowasm/internal/pkgmgr"
	"github.com/paulgrammer/gowasm/internal/tsmap"
)

// FileName is the config file every command reads.
const FileName = "gowasm.yaml"

// NPM holds the fields copied verbatim into the generated package.json.
type NPM struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description,omitempty"`
	License     string `yaml:"license,omitempty"`
	Author      string `yaml:"author,omitempty"`
	Repository  string `yaml:"repository,omitempty"`
}

// PackageSpec is one Go package the project exposes.
//
// It is written either as a bare path, which takes its namespace from the
// directory name, or as a mapping when two packages would otherwise share one:
//
//	packages:
//	  - ./pkg/lib
//	  - path: ./internal/api
//	    as: admin
type PackageSpec struct {
	Path string `yaml:"path"`
	As   string `yaml:"as,omitempty"`

	// ns is the resolved namespace, filled in once the config directory is
	// known. It is unexported so it never round-trips into the file.
	ns string
}

// UnmarshalYAML accepts either form. Ordinary YAML, rather than a bespoke
// "path:namespace" string that would have to be documented and parsed.
func (p *PackageSpec) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		return node.Decode(&p.Path)
	}
	// The alias sheds the custom unmarshaller, so this does not recurse.
	type plain PackageSpec
	var v plain
	if err := node.Decode(&v); err != nil {
		return err
	}
	*p = PackageSpec(v)
	return nil
}

// MarshalYAML writes the short form back when there is no namespace override,
// so a config written by 'gowasm init' reads the way the docs show it.
func (p PackageSpec) MarshalYAML() (any, error) {
	if p.As == "" {
		return p.Path, nil
	}
	type plain PackageSpec
	return plain(p), nil
}

// Namespace is the TypeScript name this package's exports live under.
func (p PackageSpec) Namespace() string { return p.ns }

// Config is the whole of gowasm.yaml.
type Config struct {
	Packages []PackageSpec `yaml:"packages"`
	// Package is the removed singular form, kept only so an old config fails
	// with an explanation. Dropping the field outright would make YAML ignore
	// the key, and the build would generate nothing and say nothing.
	Package string   `yaml:"package,omitempty"`
	Out     string   `yaml:"out"`
	NPM     NPM      `yaml:"npm"`
	Targets []string `yaml:"targets"`
	// PackageManager runs the generated package's install, build and publish.
	// It is recorded rather than detected on each run, so a project does not
	// change behaviour because of what happens to be installed.
	PackageManager string `yaml:"packageManager,omitempty"`
	Int64          string `yaml:"int64,omitempty"`

	// Dir is the directory holding the config; every relative path resolves
	// against it, so commands work from any subdirectory.
	Dir string `yaml:"-"`
}

// Find walks up from start looking for gowasm.yaml.
func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s found in %s or any parent directory; run 'gowasm init' first", FileName, start)
		}
		dir = parent
	}
}

// Load reads and validates the config nearest to start.
func Load(start string) (*Config, error) {
	path, err := Find(start)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	c.Dir = filepath.Dir(path)
	c.applyDefaults()
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	// An absent key means the module root; an empty list is a mistake, and
	// Validate says so. YAML tells the two apart by leaving the slice nil.
	if c.Packages == nil && c.Package == "" {
		c.Packages = []PackageSpec{{Path: "."}}
	}
	c.SetPackages(c.Packages)
	if c.Out == "" {
		c.Out = "./node"
	}
	if c.NPM.Version == "" {
		c.NPM.Version = "0.1.0"
	}
	if len(c.Targets) == 0 {
		c.Targets = []string{"node", "browser"}
	}
	if c.Int64 == "" {
		c.Int64 = string(tsmap.Int64Number)
	}
	if c.PackageManager == "" {
		c.PackageManager = string(pkgmgr.NPM)
	}
}

// SetPackages assigns the package list and resolves each namespace.
//
// Namespaces depend on the config directory, so they cannot be worked out while
// the file is being decoded. Anything building a Config by hand -- 'gowasm
// init' -- goes through here so Validate has something to check.
func (c *Config) SetPackages(specs []PackageSpec) {
	c.Packages = specs
	for i := range c.Packages {
		c.Packages[i].ns = c.namespaceFor(c.Packages[i])
	}
}

// PackagePaths lists the declared package paths.
func (c *Config) PackagePaths() []string {
	out := make([]string, 0, len(c.Packages))
	for _, p := range c.Packages {
		out = append(out, p.Path)
	}
	return out
}

// namespaceFor derives a package's TypeScript namespace from its path, unless
// one was given explicitly.
func (c *Config) namespaceFor(p PackageSpec) string {
	if p.As != "" {
		return p.As
	}
	base := filepath.Base(filepath.Clean(p.Path))
	// At the module root the directory name is what the project goes by.
	if base == "." || base == string(filepath.Separator) {
		base = filepath.Base(c.Dir)
	}
	return tsmap.Identifier(base)
}

// Multi reports whether exports are namespaced by package. A single package
// keeps the flat API it has always had; namespacing it would be ceremony with
// nothing to disambiguate.
func (c *Config) Multi() bool { return len(c.Packages) > 1 }

// reservedNamespaces are the names the generated entry point already exports,
// so a package namespace cannot take them.
var reservedNamespaces = map[string]string{
	"createClient": "the generated entry point exports createClient()",
	"dispose":      "the generated entry point exports dispose()",
	"GoError":      "the generated entry point exports GoError",
	"Client":       "the generated entry point exports the Client type",
	"LoadOptions":  "the generated entry point exports the LoadOptions type",
}

// Validate reports the first problem that would make generation fail.
func (c *Config) Validate() error {
	if c.Package != "" {
		return fmt.Errorf("package: has been replaced by packages:, which takes a list:\n\npackages:\n  - %s", c.Package)
	}
	if len(c.Packages) == 0 {
		return fmt.Errorf("packages: at least one Go package is required")
	}
	seen := map[string]string{}
	for _, p := range c.Packages {
		if strings.TrimSpace(p.Path) == "" {
			return fmt.Errorf("packages: every entry needs a path")
		}
		ns := p.Namespace()
		if !tsmap.IsIdentifier(ns) {
			return fmt.Errorf("packages: %s maps to the namespace %q, which is not a usable TypeScript name; add 'as:' to give it one", p.Path, ns)
		}
		if why, taken := reservedNamespaces[ns]; taken {
			return fmt.Errorf("packages: %s maps to the namespace %q, but %s; add 'as:' to give it another", p.Path, ns, why)
		}
		if prev, dup := seen[ns]; dup {
			return fmt.Errorf("packages: %s and %s both map to the namespace %q; add 'as:' to one of them", prev, p.Path, ns)
		}
		seen[ns] = p.Path
	}
	if c.NPM.Name == "" {
		return fmt.Errorf("npm.name is required")
	}
	if err := ValidateNPMName(c.NPM.Name); err != nil {
		return fmt.Errorf("npm.name: %w", err)
	}
	for _, t := range c.Targets {
		if t != "node" && t != "browser" {
			return fmt.Errorf("targets: unknown target %q, want node or browser", t)
		}
	}
	if _, err := pkgmgr.Parse(c.PackageManager); err != nil {
		return fmt.Errorf("packageManager: %w", err)
	}
	switch tsmap.Int64Mode(c.Int64) {
	case tsmap.Int64Number, tsmap.Int64String:
	default:
		return fmt.Errorf("int64: want %q or %q, got %q", tsmap.Int64Number, tsmap.Int64String, c.Int64)
	}
	return nil
}

// Int64Mode is the validated int64 setting.
func (c *Config) Int64Mode() tsmap.Int64Mode { return tsmap.Int64Mode(c.Int64) }

// Manager is the validated package manager.
func (c *Config) Manager() pkgmgr.Manager {
	m, err := pkgmgr.Parse(c.PackageManager)
	if err != nil {
		return pkgmgr.NPM
	}
	return m
}

// OutDir is the absolute path of the generated npm package.
func (c *Config) OutDir() string { return c.abs(c.Out) }

// BridgeDir is the path the generated Go main package appears at during a
// build. It is virtual: the directory is never created, and the file is
// supplied to the go command through an overlay. It sits outside the scanned
// package so that package always type-checks, generated or not.
func (c *Config) BridgeDir() string { return c.abs(".gowasm/wasmmain") }

func (c *Config) abs(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(c.Dir, p)
}

// Namespace is the prefix for the globals each instance installs itself under,
// derived from the unscoped npm name so two gowasm packages cannot collide.
func (c *Config) Namespace() string {
	name := c.NPM.Name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" || unicode.IsDigit(rune(out[0])) {
		out = "pkg" + out
	}
	return out
}

// Write saves the config, creating parent directories as needed.
func (c *Config) Write(dir string) (string, error) {
	out, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, FileName)
	header := "# Generated by 'gowasm init'. Edit freely, then run 'gowasm build'.\n"
	if err := os.WriteFile(path, append([]byte(header), out...), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ValidateNPMName applies npm's own package-name rules.
func ValidateNPMName(name string) error {
	if name == "" {
		return fmt.Errorf("must not be empty")
	}
	if len(name) > 214 {
		return fmt.Errorf("must be 214 characters or fewer")
	}
	if strings.ToLower(name) != name {
		return fmt.Errorf("must be lowercase")
	}
	if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
		return fmt.Errorf("must not start with . or _")
	}

	body := name
	if strings.HasPrefix(name, "@") {
		scope, rest, ok := strings.Cut(name[1:], "/")
		if !ok || scope == "" || rest == "" {
			return fmt.Errorf("scoped names look like @scope/name")
		}
		if err := validNameChars(scope); err != nil {
			return fmt.Errorf("scope: %w", err)
		}
		body = rest
	}
	return validNameChars(body)
}

func validNameChars(s string) error {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == '~':
		default:
			return fmt.Errorf("invalid character %q; use letters, digits, and - _ . ~", r)
		}
	}
	return nil
}
