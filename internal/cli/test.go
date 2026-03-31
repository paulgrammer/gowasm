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
	fmt.Fprintln(out, "running tests")
	return r.Run(cfg.OutDir(), nil, "npm", "test", "--silent")
}
