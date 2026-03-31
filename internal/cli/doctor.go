package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/paulgrammer/gowasm/internal/goenv"
	"github.com/paulgrammer/gowasm/internal/runner"
)

// doctor reports whether the local toolchain can build and run the output,
// checking every prerequisite rather than stopping at the first problem.
func doctor(dir string, out io.Writer) error {
	var problems int
	report := func(ok bool, label, detail string) {
		mark := "ok  "
		if !ok {
			mark = "FAIL"
			problems++
		}
		fmt.Fprintf(out, "  %s  %-22s %s\n", mark, label, detail)
	}

	fmt.Fprintln(out, "checking the toolchain")

	env, err := goenv.Detect()
	if err != nil {
		report(false, "go", err.Error())
		return fmt.Errorf("%d problem(s) found", problems)
	}
	report(true, "go", env.GOVERSION)

	// lib/wasm replaced misc/wasm in Go 1.24; older layouts still work but the
	// exec wrapper path differs, so say which one was found.
	report(true, "wasm support files", env.WasmLib)

	if _, err := os.Stat(env.WasmExecJS()); err != nil {
		report(false, "wasm_exec.js", "missing: "+env.WasmExecJS())
	} else {
		report(true, "wasm_exec.js", filepath.Base(env.WasmExecJS()))
	}

	if _, err := os.Stat(env.JSExecWrapper()); err != nil {
		report(false, "go_js_wasm_exec", "missing: "+env.JSExecWrapper())
	} else {
		report(true, "go_js_wasm_exec", filepath.Base(env.JSExecWrapper()))
	}

	if _, ok := runner.Look("node"); !ok {
		report(false, "node", "not found on PATH")
	} else {
		v, _ := runner.Output(dir, "node", "--version")
		ok, detail := checkNodeVersion(v)
		report(ok, "node", detail)
	}

	if _, ok := runner.Look("npm"); !ok {
		report(false, "npm", "not found on PATH")
	} else {
		v, _ := runner.Output(dir, "npm", "--version")
		report(true, "npm", v)
	}

	if problems > 0 {
		return fmt.Errorf("%d problem(s) found", problems)
	}
	fmt.Fprintln(out, "everything needed is present")
	return nil
}

// checkNodeVersion requires Node 20: the generated package uses native ESM plus
// the built-in test runner, and the generated tests are TypeScript run through
// type stripping, which needs a recent runtime.
func checkNodeVersion(v string) (bool, string) {
	major, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".", 2)[0])
	if err != nil {
		return false, "unrecognized version " + v
	}
	if major < 20 {
		return false, v + " (need 20 or newer)"
	}
	if major < 22 {
		return true, v + " (22+ recommended: runs the TypeScript tests without a build step)"
	}
	return true, v
}
