// Package runner executes the external tools gowasm drives.
package runner

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runner runs commands, optionally echoing them.
type Runner struct {
	Verbose bool
	Stdout  *os.File
	Stderr  *os.File
}

func New(verbose bool) *Runner {
	return &Runner{Verbose: verbose, Stdout: os.Stdout, Stderr: os.Stderr}
}

// Run executes name with args in dir, streaming output.
func (r *Runner) Run(dir string, env []string, name string, args ...string) error {
	if r.Verbose {
		fmt.Fprintf(r.Stderr, "  $ %s %s\n", name, strings.Join(args, " "))
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	cmd.Stdout = r.Stdout
	cmd.Stderr = r.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

// Output runs a command and captures stdout, for probes rather than builds.
func Output(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// Look reports whether a tool is on PATH.
func Look(name string) (string, bool) {
	p, err := exec.LookPath(name)
	return p, err == nil
}
