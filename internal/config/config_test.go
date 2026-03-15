package config

import (
	"os"
	"path/filepath"
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
	if cfg.Package != "." || cfg.Out != "./node" {
		t.Errorf("paths = %q %q, want . and ./node", cfg.Package, cfg.Out)
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
		"missing name":   "package: .\n",
		"bad target":     "npm:\n  name: x\ntargets: [node, deno]\n",
		"bad int64 mode": "npm:\n  name: x\nint64: bigint\n",
		"bad npm name":   "npm:\n  name: Bad Name\n",
	}
	for why, body := range cases {
		dir := t.TempDir()
		write(t, filepath.Join(dir, FileName), body)
		if _, err := Load(dir); err == nil {
			t.Errorf("%s: expected an error", why)
		}
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
