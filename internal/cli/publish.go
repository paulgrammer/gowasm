package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/paulgrammer/gowasm/internal/config"
	"github.com/paulgrammer/gowasm/internal/goenv"
	"github.com/paulgrammer/gowasm/internal/runner"
)

// publish hands the generated package to npm.
//
// It is a thin proxy: everything after `--` is passed to npm untouched, so
// --access, --tag, --otp, --dry-run and the rest behave exactly as they always
// do. gowasm adds no flags of its own to the npm command line.
//
// It does rebuild first, because the alternative is publishing whatever happens
// to be sitting in dist/ — possibly compiled from Go that has since changed, or
// by a different toolchain. Pass -no-build to skip that and publish exactly
// what is on disk.
func publish(cfg *config.Config, env *goenv.Env, r *runner.Runner, out io.Writer, npmArgs []string, opts genOptions, skipBuild bool) error {
	if skipBuild {
		fmt.Fprintln(out, "skipping the build; publishing what is already in the output directory")
	} else if err := build(cfg, env, r, out, opts); err != nil {
		return err
	}

	outDir := cfg.OutDir()
	m := cfg.Manager()
	if !m.Available() {
		return fmt.Errorf("%s is not on PATH", m)
	}

	name, version, err := manifestIdentity(outDir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(outDir, "dist")); err != nil {
		return fmt.Errorf("%s has no dist/ to publish; run 'gowasm build' first", rel(cfg.Dir, outDir))
	}

	fmt.Fprintf(out, "publishing %s@%s from %s with %s\n", name, version, rel(cfg.Dir, outDir), m)

	return r.Run(outDir, nil, string(m), m.PublishArgs("", npmArgs)...)
}

// manifestIdentity reads back what is about to be published, so the name and
// version are stated before anything leaves the machine.
func manifestIdentity(outDir string) (name, version string, err error) {
	path := filepath.Join(outDir, "package.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("reading %s: %w (run 'gowasm build' first)", path, err)
	}
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", "", fmt.Errorf("parsing %s: %w", path, err)
	}
	return manifest.Name, manifest.Version, nil
}
