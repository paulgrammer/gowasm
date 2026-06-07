package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateNPMName(t *testing.T) {
	valid := []string{"urls", "my-pkg", "my_pkg", "a.b", "@acme/urls", "@a-b/c.d", "x0"}
	for _, name := range valid {
		if err := ValidateNPMName(name); err != nil {
			t.Errorf("%q should be valid: %v", name, err)
		}
	}

	invalid := map[string]string{
		"":          "empty",
		"MyPkg":     "uppercase",
		".hidden":   "leading dot",
		"_private":  "leading underscore",
		"has space": "space",
		"@scope":    "scope without a name",
		"@/name":    "empty scope",
		"@scope/":   "empty name",
		"has/slash": "unscoped slash",
		"emoji-🎉":   "non-URL-safe character",
	}
	for name, why := range invalid {
		if err := ValidateNPMName(name); err == nil {
			t.Errorf("%q should be rejected (%s)", name, why)
		}
	}

	long := make([]byte, 215)
	for i := range long {
		long[i] = 'a'
	}
	if err := ValidateNPMName(string(long)); err == nil {
		t.Error("a name over 214 characters should be rejected")
	}
}

func TestNamespaceIsSafeAndDistinct(t *testing.T) {
	cases := map[string]string{
		"urls":                "urls",
		"@acme/urls":          "urls",
		"@acme/my-pkg":        "my_pkg",
		"gowasm-example-blob": "gowasm_example_blob",
		"@scope/9lives":       "pkg9lives",
	}
	for name, want := range cases {
		c := &Config{NPM: NPM{Name: name}}
		if got := c.Namespace(); got != want {
			t.Errorf("Namespace(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileName), "npm:\n  name: urls\n")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.PackagePaths(); len(got) != 1 || got[0] != "." || cfg.Out != "./node" {
		t.Errorf("paths = %q %q, want [.] and ./node", got, cfg.Out)
	}
	if cfg.Multi() {
		t.Error("one package must keep the flat API")
	}
	if cfg.NPM.Version != "0.1.0" {
		t.Errorf("version = %q, want 0.1.0", cfg.NPM.Version)
	}
	if len(cfg.Targets) != 2 {
		t.Errorf("targets = %v, want both by default", cfg.Targets)
	}
	if cfg.Int64Mode() != "number" {
		t.Errorf("int64 = %q, want number", cfg.Int64Mode())
	}
}

func TestLoadRejectsBadConfig(t *testing.T) {
	cases := map[string]string{
		"missing name":                                "packages: [.]\n",
		"the removed singular key":                    "package: .\nnpm:\n  name: x\n",
		"no packages at all":                          "packages: []\nnpm:\n  name: x\n",
		"an entry with no path":                       "packages:\n  - as: lib\nnpm:\n  name: x\n",
		"two packages, one namespace":                 "packages: [./a/lib, ./b/lib]\nnpm:\n  name: x\n",
		"a namespace that is a reserved word":         "packages:\n  - path: ./a\n    as: new\nnpm:\n  name: x\n",
		"a namespace the entry point already exports": "packages:\n  - path: ./a\n    as: createClient\n  - ./b\nnpm:\n  name: x\n",
		"bad target":                                  "npm:\n  name: x\ntargets: [node, deno]\n",
		"bad int64 mode":                              "npm:\n  name: x\nint64: bigint\n",
		"bad npm name":                                "npm:\n  name: Bad Name\n",
	}
	for why, body := range cases {
		dir := t.TempDir()
		write(t, filepath.Join(dir, FileName), body)
		if _, err := Load(dir); err == nil {
			t.Errorf("%s: expected an error", why)
		}
	}
}

// The two spellings of an entry have to mean the same thing, or the short form
// is a trap rather than a convenience.
func TestPackagesAcceptBothForms(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileName), "packages:\n  - ./pkg/lib\n  - path: ./internal/api\n    as: admin\nnpm:\n  name: x\n")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Multi() {
		t.Error("two packages must namespace their exports")
	}
	want := []struct{ path, ns string }{
		{"./pkg/lib", "lib"},
		{"./internal/api", "admin"},
	}
	for i, w := range want {
		got := cfg.Packages[i]
		if got.Path != w.path || got.Namespace() != w.ns {
			t.Errorf("packages[%d] = %q as %q, want %q as %q", i, got.Path, got.Namespace(), w.path, w.ns)
		}
	}
}

// An old config must fail loudly. Removing the field alone would make YAML
// ignore the key, and the build would generate nothing and say nothing.
func TestTheRemovedPackageKeyExplainsItself(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, FileName), "package: ./urls\nnpm:\n  name: x\n")

	_, err := Load(dir)
	if err == nil {
		t.Fatal("an old config should not load")
	}
	if !strings.Contains(err.Error(), "packages:") {
		t.Errorf("the error should name the replacement, got: %v", err)
	}
}

// A path with punctuation in it still has to yield a legal TypeScript name.
func TestNamespacesAreDerivedFromTheDirectory(t *testing.T) {
	cases := map[string]string{
		"./pkg/lib":     "lib",
		"./go-qrcode":   "goqrcode",
		"./pkg/2fast":   "pkg2fast",
		"internal/api/": "api",
	}
	for path, want := range cases {
		c := &Config{Dir: "/tmp/project"}
		c.SetPackages([]PackageSpec{{Path: path}})
		if got := c.Packages[0].Namespace(); got != want {
			t.Errorf("namespace of %q = %q, want %q", path, got, want)
		}
	}

	// At the module root the directory name is what the project goes by.
	c := &Config{Dir: "/tmp/my-project"}
	c.SetPackages([]PackageSpec{{Path: "."}})
	if got := c.Packages[0].Namespace(); got != "myproject" {
		t.Errorf("namespace of . = %q, want myproject", got)
	}
}

// A config written back out has to load again, or 'gowasm init' produces
// something the next command rejects.
func TestWrittenConfigRoundTrips(t *testing.T) {
	dir := t.TempDir()
	c := &Config{Dir: dir, Out: "./node", NPM: NPM{Name: "x", Version: "0.1.0"}, Targets: []string{"node"}}
	c.SetPackages([]PackageSpec{{Path: "./a"}, {Path: "./b", As: "beta"}})
	if _, err := c.Write(dir); err != nil {
		t.Fatal(err)
	}

	back, err := Load(dir)
	if err != nil {
		t.Fatalf("a config gowasm wrote does not load: %v", err)
	}
	if len(back.Packages) != 2 ||
		back.Packages[0].Path != "./a" || back.Packages[0].Namespace() != "a" ||
		back.Packages[1].Path != "./b" || back.Packages[1].Namespace() != "beta" {
		t.Errorf("round trip lost the packages: %+v", back.Packages)
	}
}

func TestFindWalksUpwards(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, FileName), "npm:\n  name: urls\n")

	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	// Commands should work from any subdirectory of the project.
	cfg, err := Load(nested)
	if err != nil {
		t.Fatalf("loading from a subdirectory: %v", err)
	}
	if filepath.Base(cfg.Dir) != filepath.Base(root) {
		t.Errorf("Dir = %q, want the directory holding the config", cfg.Dir)
	}

	if _, err := Find(t.TempDir()); err == nil {
		t.Error("Find should fail when there is no config anywhere above")
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
