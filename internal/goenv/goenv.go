// Package goenv locates the Go toolchain's WebAssembly support files.
package goenv

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Env describes the local Go installation.
type Env struct {
	GOROOT    string
	GOVERSION string
	// WasmLib is the directory holding wasm_exec.js and the exec wrappers.
	WasmLib string
}

// Detect queries the go command.
func Detect() (*Env, error) {
	out, err := exec.Command("go", "env", "GOROOT", "GOVERSION").Output()
	if err != nil {
		return nil, fmt.Errorf("running 'go env': %w (is Go installed and on PATH?)", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("unexpected output from 'go env'")
	}
	e := &Env{GOROOT: strings.TrimSpace(lines[0]), GOVERSION: strings.TrimSpace(lines[1])}

	// Go 1.24 moved these from misc/wasm to lib/wasm.
	for _, dir := range []string{
		filepath.Join(e.GOROOT, "lib", "wasm"),
		filepath.Join(e.GOROOT, "misc", "wasm"),
	} {
		if _, err := os.Stat(filepath.Join(dir, "wasm_exec.js")); err == nil {
			e.WasmLib = dir
			break
		}
	}
	if e.WasmLib == "" {
		return nil, fmt.Errorf("wasm_exec.js not found under %s; is this a complete Go installation?", e.GOROOT)
	}
	return e, nil
}

// WasmExecJS is the path to the runtime bridge that must be vendored.
func (e *Env) WasmExecJS() string { return filepath.Join(e.WasmLib, "wasm_exec.js") }

// JSExecWrapper is the -exec helper that runs js/wasm test binaries under Node.
//
// Callers should pass this to `go test -exec=` explicitly rather than relying on
// PATH lookup: cmd/go does not add lib/wasm to PATH, so a stale wrapper from an
// older Go install would be found first and resolve into the removed misc/wasm.
func (e *Env) JSExecWrapper() string { return filepath.Join(e.WasmLib, "go_js_wasm_exec") }

// VendorWasmExec copies wasm_exec.js to dst, stamped with the toolchain that
// produced it.
//
// It must be vendored rather than installed from npm: the file is part of the
// Go runtime ABI and has to match the compiler exactly, which no independently
// versioned package can guarantee.
func (e *Env) VendorWasmExec(dst string) error {
	src, err := os.ReadFile(e.WasmExecJS())
	if err != nil {
		return fmt.Errorf("reading %s: %w", e.WasmExecJS(), err)
	}
	header := fmt.Sprintf(
		"// Vendored by gowasm from %s\n// Toolchain: %s — must match the compiler that built main.wasm.\n",
		e.WasmExecJS(), e.GOVERSION)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, append([]byte(header), src...), 0o644)
}
