package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/paulgrammer/gowasm/internal/config"
	"github.com/paulgrammer/gowasm/internal/goenv"
	"github.com/paulgrammer/gowasm/internal/overlay"
	"github.com/paulgrammer/gowasm/internal/pkgmgr"
	"github.com/paulgrammer/gowasm/internal/runner"
	"github.com/paulgrammer/gowasm/internal/wasmbridge"
)

// build generates, compiles the WebAssembly module, and builds the npm package.
func build(cfg *config.Config, env *goenv.Env, r *runner.Runner, out io.Writer, opts genOptions) error {
	res, err := generate(cfg, env, out, opts)
	if err != nil {
		return err
	}

	outDir := cfg.OutDir()
	wasmPath := filepath.Join(outDir, "src", "main.wasm")
	if err := os.MkdirAll(filepath.Dir(wasmPath), 0o755); err != nil {
		return err
	}

	// The generated package is compiled from a virtual path inside the module:
	// the registrations, and the runtime that serves them. Nothing is written to
	// the user's repository, so there is no generated directory to gitignore, no
	// stale copy to drift, and no gowasm entry in their go.mod.
	runtimeFiles, err := wasmbridge.Source()
	if err != nil {
		return err
	}
	virtual := map[string][]byte{
		filepath.Join(cfg.BridgeDir(), "main_gen.go"): res.Bridge,
	}
	for _, f := range runtimeFiles {
		virtual[filepath.Join(cfg.BridgeDir(), f.Name)] = f.Content
	}

	ov, err := overlay.New(virtual)
	if err != nil {
		return err
	}
	defer ov.Close()

	fmt.Fprintf(out, "compiling %s -> %s\n", packageList(cfg), rel(cfg.Dir, wasmPath))
	err = r.Run(cfg.Dir, []string{"GOOS=js", "GOARCH=wasm"},
		"go", "build",
		"-overlay", ov.Path,
		"-trimpath",
		// Dropping the symbol table and DWARF info meaningfully shrinks the
		// module; Go panics stay readable because the runtime keeps its own
		// function-name table.
		"-ldflags=-s -w",
		"-o", wasmPath,
		"./"+filepath.ToSlash(mustRel(cfg.Dir, cfg.BridgeDir())),
	)
	if err != nil {
		return err
	}

	if info, statErr := os.Stat(wasmPath); statErr == nil {
		fmt.Fprintf(out, "  main.wasm is %s\n", humanBytes(info.Size()))
	}

	return buildNPM(cfg, r, out)
}

func buildNPM(cfg *config.Config, r *runner.Runner, out io.Writer) error {
	outDir := cfg.OutDir()
	m := cfg.Manager()
	if !m.Available() {
		return fmt.Errorf("%s is not on PATH; it is needed to compile the TypeScript.\n"+
			"  Install it, or set packageManager in gowasm.yaml to one of: %s",
			m, strings.Join(pkgmgr.Names(), ", "))
	}

	if _, err := os.Stat(filepath.Join(outDir, "node_modules")); os.IsNotExist(err) {
		fmt.Fprintf(out, "installing dev dependencies with %s\n", m)
		if err := r.Run(outDir, nil, string(m), append(m.InstallArgs(), m.Quiet()...)...); err != nil {
			return err
		}
	}

	fmt.Fprintln(out, "compiling TypeScript")
	return r.Run(outDir, nil, string(m), append(m.RunArgs("build"), m.Quiet()...)...)
}

// packageList names what is being compiled, so a multi-package build says which
// packages went into the single module it produces.
func packageList(cfg *config.Config) string {
	paths := make([]string, 0, len(cfg.Packages))
	for _, p := range cfg.Packages {
		paths = append(paths, p.Path)
	}
	return strings.Join(paths, ", ")
}

func mustRel(base, path string) string {
	r, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return r
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
