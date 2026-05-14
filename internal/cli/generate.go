package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/paulgrammer/gowasm/internal/config"
	"github.com/paulgrammer/gowasm/internal/fixtures"
	"github.com/paulgrammer/gowasm/internal/gen/gobridge"
	"github.com/paulgrammer/gowasm/internal/gen/scaffold"
	"github.com/paulgrammer/gowasm/internal/gen/tsclient"
	"github.com/paulgrammer/gowasm/internal/gen/tstest"
	"github.com/paulgrammer/gowasm/internal/goenv"
	"github.com/paulgrammer/gowasm/internal/scan"
)

// result reports what a generation pass produced.
type result struct {
	Module  *scan.Module
	Written int
	Same    int

	// Bridge is the generated Go source that registers each exported function.
	// It is handed to the build as an overlay rather than written into the
	// user's repository; see internal/overlay.
	Bridge []byte
}

// genOptions tunes a generation pass.
type genOptions struct {
	// BridgeOut, when set, also writes the generated Go bridge there. It is
	// otherwise never written to disk, and exists only for inspection.
	BridgeOut string
}

// generate runs the whole codegen pipeline: scan the Go package, then emit the
// Go bridge, the TypeScript package and its tests.
func generate(cfg *config.Config, env *goenv.Env, out io.Writer, opts genOptions) (*result, error) {
	mod, err := scan.Load(scan.Options{
		Dir:     cfg.Dir,
		Pattern: cfg.Package,
		Int64:   cfg.Int64Mode(),
	})
	if err != nil {
		return nil, err
	}

	outDir := cfg.OutDir()
	srcDir := filepath.Join(outDir, "src")
	testDir := filepath.Join(outDir, "test")

	// Clear the directories gowasm owns outright, so a renamed or deleted Go
	// function cannot leave a stale file behind. Hand-written tests live in
	// files without the .gen infix and are never touched.
	if err := clearDir(filepath.Join(srcDir, "generated")); err != nil {
		return nil, err
	}
	if err := clearDir(filepath.Join(srcDir, "runtime")); err != nil {
		return nil, err
	}
	if err := removeGlob(srcDir, "index.*.ts"); err != nil {
		return nil, err
	}
	if err := removeGlob(testDir, "*.gen.test.ts"); err != nil {
		return nil, err
	}

	w := &writer{}

	bridge, err := gobridge.Generate(mod, cfg.Namespace())
	if err != nil {
		return nil, err
	}
	if opts.BridgeOut != "" {
		if err := w.write(opts.BridgeOut, bridge); err != nil {
			return nil, err
		}
		fmt.Fprintf(out, "wrote the Go bridge to %s\n", rel(cfg.Dir, opts.BridgeOut))
	}

	tsFiles, err := tsclient.Generate(mod, tsclient.Options{
		Namespace: cfg.Namespace(),
		Targets:   cfg.Targets,
	})
	if err != nil {
		return nil, err
	}
	for _, f := range tsFiles {
		if err := w.write(filepath.Join(srcDir, f.Path), f.Content); err != nil {
			return nil, err
		}
	}

	scaffoldFiles, err := scaffold.Generate(cfg, mod)
	if err != nil {
		return nil, err
	}
	for _, f := range scaffoldFiles {
		if err := w.write(filepath.Join(outDir, f.Path), f.Content); err != nil {
			return nil, err
		}
	}

	testFiles, err := tstest.Generate(mod, cfg.Targets)
	if err != nil {
		return nil, err
	}
	for _, f := range testFiles {
		if err := w.write(filepath.Join(testDir, f.Path), f.Content); err != nil {
			return nil, err
		}
	}

	if err := writeFixtureTests(cfg, env, mod, testDir, w, out); err != nil {
		return nil, err
	}

	// wasm_exec.js is part of the Go runtime ABI and must match the compiler
	// that builds main.wasm, so it is copied from GOROOT rather than installed
	// from npm.
	vendored := filepath.Join(srcDir, "vendor", "wasm_exec.js")
	if err := env.VendorWasmExec(vendored); err != nil {
		return nil, err
	}

	for _, name := range tsclient.ReservedNames(mod) {
		fmt.Fprintf(out, "  note: %s is a JavaScript reserved word, so it has no named export; "+
			"reach it through createClient()\n", name)
	}

	fmt.Fprintf(out, "generated %d file(s) in %s\n", w.written+1, rel(cfg.Dir, outDir))
	return &result{Module: mod, Written: w.written, Same: w.same, Bridge: bridge}, nil
}

// writeFixtureTests records the package's Go Example calls by running them, and
// emits the behavioural suite. A package with no Examples simply gets none.
func writeFixtureTests(cfg *config.Config, env *goenv.Env, mod *scan.Module, testDir string, w *writer, out io.Writer) error {
	calls, skipped, err := scan.LoadExamples(scan.Options{
		Dir:     cfg.Dir,
		Pattern: cfg.Package,
		Int64:   cfg.Int64Mode(),
	}, mod)
	if err != nil {
		return err
	}

	// Calls the recorder cannot reproduce are named rather than dropped in
	// silence, so it is always clear what is and is not covered.
	for _, s := range skipped {
		fmt.Fprintf(out, "  note: %s calls %s with %s; no fixture recorded (%s)\n",
			s.Example, s.GoFunc, s.Reason, s.Pos)
	}
	if len(calls) == 0 {
		return nil
	}

	recorded, err := fixtures.Record(cfg.Dir, env.JSExecWrapper(), mod, calls)
	if err != nil {
		return err
	}

	// A call whose result changes between runs cannot be asserted on. Naming it
	// is more useful than emitting a test that fails at random.
	kept := recorded[:0]
	for _, f := range recorded {
		if f.NonDeterministic {
			fmt.Fprintf(out, "  note: %s calls %s, whose result differs between runs; no fixture recorded (%s)\n",
				f.Example, f.JSFunc, f.Pos)
			continue
		}
		kept = append(kept, f)
	}
	recorded = kept

	files, err := tstest.GenerateFixtures(mod, recorded)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := w.write(filepath.Join(testDir, f.Path), f.Content); err != nil {
			return err
		}
	}
	if len(recorded) == 0 {
		return nil
	}
	fmt.Fprintf(out, "recorded %d fixture(s) from Go Example functions\n", len(recorded))
	return nil
}

// writer writes files, skipping ones whose content is already correct so that
// repeated runs leave mtimes alone and stay diff-clean.
type writer struct {
	written int
	same    int
}

func (w *writer) write(path string, content []byte) error {
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(content) {
		w.same++
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	w.written++
	return nil
}

func clearDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func removeGlob(dir, pattern string) error {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return err
	}
	sort.Strings(matches)
	for _, m := range matches {
		if err := os.Remove(m); err != nil {
			return err
		}
	}
	return nil
}

// rel renders a path relative to base when that is shorter and clearer.
func rel(base, path string) string {
	r, err := filepath.Rel(base, path)
	if err != nil || strings.HasPrefix(r, "..") {
		return path
	}
	return "./" + r
}
