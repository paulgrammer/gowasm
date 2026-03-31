// Package cli implements the gowasm commands.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/paulgrammer/gowasm/internal/config"
	"github.com/paulgrammer/gowasm/internal/goenv"
	"github.com/paulgrammer/gowasm/internal/runner"
)

// Build information, stamped by the linker at release time. See the ldflags in
// the Makefile.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

const usage = `gowasm turns a Go package into a typed npm package built on WebAssembly.

Usage:
  gowasm <command> [flags]

Commands:
  init        Create gowasm.yaml, asking the questions npm init asks
  generate    Scan the Go package and write the TypeScript package
  build       Generate, compile to WebAssembly, and build the npm package
  test        Build, then run the generated tests on Node
  publish     Build, then hand the package to npm publish
  doctor      Check that the toolchain can build and run WebAssembly

Common flags:
  -C dir      Run as if started in dir
  -v          Echo the commands being run
  -y          Accept every default without asking (init only)
  -bridge f   Also write the generated Go bridge to f, for inspection
  -no-build   Publish what is on disk, without rebuilding first (publish only)
  -version    Print the version and exit

Everything after -- is passed to npm untouched:
  gowasm publish -- --access public
  gowasm publish -- --dry-run --tag next
`

// Main runs the CLI and returns a process exit code.
func Main(args []string) int {
	if err := run(args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "gowasm: %v\n", err)
		return 1
	}
	return 0
}

func run(args []string, stdout, stderr io.Writer) error {
	// Flags are accepted on either side of the command name. Go's flag package
	// stops at the first non-flag argument, so the command is split out first
	// and both halves are parsed together.
	cmd, rest := splitCommand(args)

	fs := flag.NewFlagSet("gowasm", flag.ContinueOnError)
	// For publish, an unknown flag is almost always an npm flag that needs to go
	// after --. The flag package's own message and the usage screen would both
	// bury the one line that says so, so they are silenced and the error
	// returned below is the whole output.
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	if cmd == "publish" {
		fs.SetOutput(io.Discard)
		fs.Usage = func() {}
	}

	var (
		dir         = fs.String("C", ".", "run as if started in `dir`")
		verbose     = fs.Bool("v", false, "echo the commands being run")
		showVersion = fs.Bool("version", false, "print the version and exit")
		yes         = fs.Bool("y", false, "accept every default without asking")
		bridgeOut   = fs.String("bridge", "", "also write the generated Go bridge to `file` for inspection")
		noBuild     = fs.Bool("no-build", false, "publish without rebuilding first")
	)
	if err := fs.Parse(rest); err != nil {
		// npm's own flags are common enough here to be worth naming the fix.
		if cmd == "publish" {
			return fmt.Errorf("%w\n\nnpm flags go after --, for example:\n  gowasm publish -- --access public", err)
		}
		return err
	}

	if *showVersion {
		fmt.Fprintf(stdout, "gowasm %s (%s, built %s, %s/%s)\n",
			Version, Commit, Date, runtime.GOOS, runtime.GOARCH)
		return nil
	}
	if cmd == "" {
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("no command given")
	}

	// doctor, init and help must work before there is a valid config.
	switch cmd {
	case "help":
		fmt.Fprint(stdout, usage)
		return nil
	case "doctor":
		return doctor(*dir, stdout)
	case "init":
		return initCmd(*dir, *yes, stdout)
	}

	cfg, err := config.Load(*dir)
	if err != nil {
		return err
	}
	env, err := goenv.Detect()
	if err != nil {
		return err
	}
	r := runner.New(*verbose)
	opts := genOptions{BridgeOut: *bridgeOut}

	switch cmd {
	case "generate":
		_, err := generate(cfg, env, stdout, opts)
		return err
	case "build":
		return build(cfg, env, r, stdout, opts)
	case "test":
		return testCmd(cfg, env, r, stdout, opts)
	case "publish":
		// Everything the flag package left over is destined for npm.
		return publish(cfg, env, r, stdout, fs.Args(), opts, *noBuild)
	default:
		fmt.Fprint(stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// splitCommand pulls the first bare word out of args, leaving the flags.
func splitCommand(args []string) (cmd string, rest []string) {
	rest = make([]string, 0, len(args))
	for i, a := range args {
		if cmd == "" && a != "" && a[0] != '-' {
			// A value for a flag like `-C dir` is not the command.
			if i > 0 && isFlagExpectingValue(args[i-1]) {
				rest = append(rest, a)
				continue
			}
			cmd = a
			continue
		}
		rest = append(rest, a)
	}
	return cmd, rest
}

// isFlagExpectingValue reports whether a bare `-flag` takes the next argument
// as its value. Only -C does; the rest are booleans.
func isFlagExpectingValue(prev string) bool {
	return prev == "-C" || prev == "--C"
}
