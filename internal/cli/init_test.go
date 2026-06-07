package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A new project has a module but no Go files anywhere, so rejecting a directory
// for being empty left no valid answer and the question asked itself forever.
func TestValidatePackageDirAcceptsEmptyAndMissing(t *testing.T) {
	base := t.TempDir()

	if err := os.MkdirAll(filepath.Join(base, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "afile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	validate := validatePackageDirs(base)
	cases := []struct {
		path string
		ok   bool
		why  string
	}{
		{".", true, "the module root of a new project has no Go files yet"},
		{"./empty", true, "an empty directory is one to fill in, not a mistake"},
		{"./does-not-exist", true, "a directory that will be created"},
		{"./afile", false, "a file is not a package"},
		{"", false, "no answer at all"},
		{"./empty,./does-not-exist", true, "several packages, each its own namespace"},
		{"./empty,./empty", false, "the same package twice would collide"},
		{"./empty,./afile", false, "one bad entry fails the whole list"},
	}
	for _, c := range cases {
		err := validate(c.path)
		if c.ok && err != nil {
			t.Errorf("%q should be accepted (%s): %v", c.path, c.why, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%q should be rejected (%s)", c.path, c.why)
		}
	}
}

func TestWriteStarterLeavesExistingCodeAlone(t *testing.T) {
	base := t.TempDir()
	existing := filepath.Join(base, "pkg")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	original := "package pkg\n\nfunc Mine() {}\n"
	if err := os.WriteFile(filepath.Join(existing, "mine.go"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	wrote, err := writeStarter(base, "./pkg", "example.com/demo")
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Error("a package that already has Go files must not be scaffolded over")
	}

	got, err := os.ReadFile(filepath.Join(existing, "mine.go"))
	if err != nil || string(got) != original {
		t.Error("the existing file was modified")
	}
}

func TestWriteStarterNamesThePackageAfterItsDirectory(t *testing.T) {
	base := t.TempDir()
	for _, c := range []struct{ path, want string }{
		{".", "demo"},         // the module root takes the module's last segment
		{"./api", "api"},      // a subdirectory takes its own name
		{"./my-pkg", "mypkg"}, // punctuation is not legal in a package name
	} {
		if _, err := writeStarter(base, c.path, "example.com/demo"); err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		dir := filepath.Join(base, c.path)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		// The package file and its test are named after the package, the way Go
		// pairs foo.go with foo_test.go.
		for _, want := range []string{c.want + ".go", c.want + "_test.go"} {
			var found bool
			for _, e := range entries {
				if e.Name() == want {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: expected %s among %v", c.path, want, names(entries))
			}
		}
	}
}

// The starter is what a new user runs first. If it does not build, the tool
// looks broken before they have written anything.
func TestStarterProjectBuildsAndItsExamplesPass(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a module")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go toolchain")
	}

	base := t.TempDir()
	const module = "example.com/fresh"

	goMod := "module " + module + "\n\ngo 1.24\n"
	if err := os.WriteFile(filepath.Join(base, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := writeStarter(base, ".", module); err != nil {
		t.Fatal(err)
	}

	// Examples carry their own expected output, so `go test` passing means the
	// starter is not merely syntactically valid but behaves as documented.
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = base
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the scaffolded project does not pass its own tests: %v\n%s", err, out)
	}

	// And it has to survive the target it will actually be compiled for.
	build := exec.Command("go", "build", "./...")
	build.Dir = base
	build.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("the scaffolded project does not build for js/wasm: %v\n%s", err, out)
	}
}

func TestGoPackageName(t *testing.T) {
	cases := map[string]string{
		"api":        "api",
		"my-pkg":     "mypkg",
		"My.Pkg":     "mypkg",
		"2fast":      "app2fast", // an identifier may not start with a digit
		"___":        "app",      // nothing usable left
		"test-asemp": "testasemp",
	}
	for in, want := range cases {
		if got := goPackageName(in); got != want {
			t.Errorf("goPackageName(%q) = %q, want %q", in, got, want)
		}
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}
