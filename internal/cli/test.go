package cli

import (
	"fmt"
	"io"

	"github.com/paulgrammer/gowasm/internal/config"
	"github.com/paulgrammer/gowasm/internal/goenv"
	"github.com/paulgrammer/gowasm/internal/runner"
)

// testCmd builds the package and runs the generated Node tests against the
// compiled output, so what is tested is what would be published.
func testCmd(cfg *config.Config, env *goenv.Env, r *runner.Runner, out io.Writer, opts genOptions) error {
	if err := build(cfg, env, r, out, opts); err != nil {
		return err
	}
	m := cfg.Manager()
	fmt.Fprintf(out, "running tests with %s\n", m)
	// Always through `run`: bun test would run Bun's own test runner and never
	// look at the package's test script.
	return r.Run(cfg.OutDir(), nil, string(m), append(m.RunArgs("test"), m.Quiet()...)...)
}
