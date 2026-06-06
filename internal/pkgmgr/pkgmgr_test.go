package pkgmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, name := range []string{"npm", "pnpm", "yarn", "bun", "  PNPM  "} {
		if _, err := Parse(name); err != nil {
			t.Errorf("Parse(%q): %v", name, err)
		}
	}
	for _, name := range []string{"", "deno", "npm2", "yarnpkg"} {
		if _, err := Parse(name); err == nil {
			t.Errorf("Parse(%q) should have failed", name)
		}
	}
}

// A lockfile is a decision the project already made. Running a different
// manager against it produces a second lockfile and two sources of truth.
func TestDetectPrefersAnExistingLockfile(t *testing.T) {
	for _, c := range []struct {
		lockfile string
		want     Manager
	}{
		{"package-lock.json", NPM},
		{"pnpm-lock.yaml", PNPM},
		{"yarn.lock", Yarn},
		{"bun.lockb", Bun},
		{"bun.lock", Bun}, // newer bun writes a text lockfile
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, c.lockfile), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := Detect(dir); got != c.want {
			t.Errorf("a directory holding %s detected as %s, want %s", c.lockfile, got, c.want)
		}
	}
}

func TestDetectFallsBackToAnInstalledManager(t *testing.T) {
	got := Detect(t.TempDir())
	if !got.Available() {
		t.Errorf("Detect chose %s, which is not installed", got)
	}
}

// bun test runs Bun's own test runner and never looks at the package's test
// script, so going through `run` is not stylistic: it is the difference between
// running the tests and silently running nothing.
func TestRunAlwaysGoesThroughRun(t *testing.T) {
	for _, m := range All() {
		args := m.RunArgs("test")
		if len(args) != 2 || args[0] != "run" || args[1] != "test" {
			t.Errorf("%s.RunArgs(test) = %v, want [run test]", m, args)
		}
	}
}

// Yarn moved publishing under an npm subcommand in version 2. Getting this
// wrong fails with "command not found" on one version or the other.
func TestYarnPublishDependsOnItsVersion(t *testing.T) {
	cases := map[string]string{
		"1.22.19": "publish",
		"3.6.4":   "npm publish",
		"4.1.0":   "npm publish",
	}
	for version, want := range cases {
		got := strings.Join(Yarn.PublishArgs(version, nil), " ")
		if got != want {
			t.Errorf("yarn %s publishes with %q, want %q", version, got, want)
		}
	}
}

// The generated directory is regenerated on every build and is normally
// gitignored, so pnpm's clean-tree check can only ever fail.
func TestPnpmPublishSkipsTheGitCheck(t *testing.T) {
	args := PNPM.PublishArgs("", nil)
	if !contains(args, "--no-git-checks") {
		t.Errorf("pnpm publish args %v should disable the git check", args)
	}
}

func TestPublishPassesExtraFlagsThrough(t *testing.T) {
	extra := []string{"--access", "public", "--tag", "next"}
	for _, m := range All() {
		args := m.PublishArgs("1.22.19", extra)
		for _, want := range extra {
			if !contains(args, want) {
				t.Errorf("%s dropped %q from %v", m, want, args)
			}
		}
	}
}

// Yarn 2 removed --silent and errors on an unknown flag; bun's differs between
// subcommands. Quieter output is not worth a failed command.
func TestQuietOnlyWhereItIsSafe(t *testing.T) {
	if len(NPM.Quiet()) == 0 || len(PNPM.Quiet()) == 0 {
		t.Error("npm and pnpm both support --silent")
	}
	if len(Yarn.Quiet()) != 0 || len(Bun.Quiet()) != 0 {
		t.Error("yarn and bun should be left alone rather than passed a flag they may reject")
	}
}

func TestLockfilesAreDistinct(t *testing.T) {
	seen := map[string]Manager{}
	for _, m := range All() {
		if other, dup := seen[m.Lockfile()]; dup {
			t.Errorf("%s and %s claim the same lockfile %q", m, other, m.Lockfile())
		}
		seen[m.Lockfile()] = m
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
