// Package pkgmgr runs the Node package manager a project has chosen.
//
// The four managers are not interchangeable at the command line, and the
// differences are the kind that fail quietly rather than loudly:
//
//   - `bun test` runs Bun's own test runner and ignores the package's test
//     script entirely, so it has to be `bun run test`.
//   - Yarn moved publishing from `yarn publish` to `yarn npm publish` in
//     version 2, so the right command depends on which yarn is installed.
//   - pnpm refuses to publish from a directory with uncommitted changes,
//     which a generated directory always has.
//
// Encoding those here means the rest of the tool can ask for "install" or
// "run build" and get something that works.
package pkgmgr

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manager is a supported Node package manager.
type Manager string

const (
	NPM  Manager = "npm"
	PNPM Manager = "pnpm"
	Yarn Manager = "yarn"
	Bun  Manager = "bun"
)

// All lists the supported managers, npm first because it is the one that is
// always present with Node.
func All() []Manager { return []Manager{NPM, PNPM, Yarn, Bun} }

// Parse validates a name.
func Parse(s string) (Manager, error) {
	for _, m := range All() {
		if string(m) == strings.TrimSpace(strings.ToLower(s)) {
			return m, nil
		}
	}
	return "", fmt.Errorf("unknown package manager %q, want npm, pnpm, yarn or bun", s)
}

// Names returns the supported names, for a prompt or an error message.
func Names() []string {
	out := make([]string, 0, len(All()))
	for _, m := range All() {
		out = append(out, string(m))
	}
	return out
}

// Lockfile is the file that identifies a project as using this manager.
func (m Manager) Lockfile() string {
	switch m {
	case PNPM:
		return "pnpm-lock.yaml"
	case Yarn:
		return "yarn.lock"
	case Bun:
		return "bun.lockb"
	default:
		return "package-lock.json"
	}
}

// Available reports whether the manager is on PATH.
func (m Manager) Available() bool {
	_, err := exec.LookPath(string(m))
	return err == nil
}

// Version returns the installed version, or an empty string.
func (m Manager) Version() string {
	out, err := exec.Command(string(m), "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Detect picks the manager a directory is already using, by its lockfile, and
// otherwise the first one installed.
//
// A lockfile is checked first because it is a decision the project has already
// made: running a different manager against it produces a second lockfile and
// two sources of truth.
func Detect(dir string) Manager {
	for _, m := range All() {
		if _, err := os.Stat(filepath.Join(dir, m.Lockfile())); err == nil {
			return m
		}
		// Bun writes a text lockfile in newer versions.
		if m == Bun {
			if _, err := os.Stat(filepath.Join(dir, "bun.lock")); err == nil {
				return m
			}
		}
	}
	for _, m := range All() {
		if m.Available() {
			return m
		}
	}
	return NPM
}

// InstallArgs installs the dependencies in the current directory.
func (m Manager) InstallArgs() []string {
	switch m {
	case Yarn:
		// `yarn install` is the same in both major versions; the bare `yarn`
		// also installs, but being explicit reads better in a log.
		return []string{"install"}
	default:
		return []string{"install"}
	}
}

// RunArgs runs a script from package.json.
//
// Always through `run`, including for test. `npm test` and `pnpm test` are
// aliases for running the script, but `bun test` is not: it runs Bun's own test
// runner and never looks at the script, which would silently do the wrong thing.
func (m Manager) RunArgs(script string) []string {
	return []string{"run", script}
}

// PublishArgs publishes the package in the current directory.
//
// version is the manager's own version, which only yarn needs; pass an empty
// string to have it looked up.
func (m Manager) PublishArgs(version string, extra []string) []string {
	switch m {
	case Yarn:
		if version == "" {
			version = m.Version()
		}
		// Yarn 2 moved publishing under an npm subcommand. Yarn 1 has no such
		// subcommand and would fail with an unhelpful "command not found".
		if strings.HasPrefix(version, "1.") || version == "" {
			return append([]string{"publish"}, extra...)
		}
		return append([]string{"npm", "publish"}, extra...)

	case PNPM:
		// pnpm refuses to publish when the directory has uncommitted changes.
		// The directory here is generated on every build and is normally
		// gitignored, so that check can only ever fail, and never for a reason
		// the user can act on.
		return append([]string{"publish", "--no-git-checks"}, extra...)

	default:
		return append([]string{"publish"}, extra...)
	}
}

// Quiet returns the flag that suppresses routine output, or nothing when the
// manager has none that is safe to pass.
func (m Manager) Quiet() []string {
	switch m {
	case NPM, PNPM:
		return []string{"--silent"}
	default:
		// Yarn 2 removed --silent and errors on an unknown flag; bun's is
		// inconsistent between subcommands. Quieter output is not worth a
		// failed command.
		return nil
	}
}
