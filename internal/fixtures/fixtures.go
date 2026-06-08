// Package fixtures records what the Go code actually returns, so the generated
// TypeScript tests assert against real values rather than guesses.
//
// The call sites come from the package's own Example functions (see
// scan.LoadExamples). This package compiles a throwaway program that performs
// those calls natively, captures each result as JSON, and hands them back. The
// expectations are therefore produced by running the user's real code: they
// cannot drift from it, and nested structs, time values and errors are all
// handled by encoding/json rather than by anything invented here.
package fixtures

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/paulgrammer/gowasm/internal/overlay"
	"github.com/paulgrammer/gowasm/internal/scan"
)

// Fixture is one recorded call.
//
// NonDeterministic marks a call whose result changed between two identical
// runs, which means it cannot be asserted on. A generated test that is flaky
// is worse than no test at all, so these are reported and skipped rather than
// emitted and left to fail intermittently.
type Fixture struct {
	NonDeterministic bool `json:"-"`

	Example string   `json:"-"`
	JSFunc  string   `json:"-"`
	Pos     string   `json:"-"`
	Args    []string `json:"-"`

	// Result is the JSON the function returned, or empty when it returns
	// nothing.
	Result string `json:"-"`
	// Error is the Go error message, when the call failed.
	Error string `json:"-"`
	Void  bool   `json:"-"`
}

// record mirrors the JSON the generated recorder prints.
type record struct {
	Args   []json.RawMessage `json:"args"`
	Result json.RawMessage   `json:"result,omitempty"`
	Error  string            `json:"error,omitempty"`
	Void   bool              `json:"void,omitempty"`
}

// Group is one package's example calls, kept together so a call can be
// rendered against the package it belongs to.
type Group struct {
	Module *scan.Module
	Calls  []scan.ExampleCall
}

// Record runs every call and captures its outcome, returning one slice of
// fixtures per group in the order the groups were given.
//
// Every package is recorded by a single generated program. Compiling for
// js/wasm is the expensive part, so a project with several packages pays for
// one build rather than one per package.
//
// execWrapper is the go_js_wasm_exec helper from GOROOT. The recorder runs
// under js/wasm, the same target the generated tests exercise, rather than on
// the host: anything that differs between the two -- timing, word size,
// available syscalls -- would otherwise be baked into an expectation that the
// tests then fail to meet.
func Record(dir, execWrapper string, groups []Group) ([][]Fixture, error) {
	total := 0
	for _, g := range groups {
		total += len(g.Calls)
	}
	if total == 0 {
		return make([][]Fixture, len(groups)), nil
	}

	src, err := renderRecorder(groups)
	if err != nil {
		return nil, err
	}

	// Same overlay trick as the build: the recorder must sit inside the user's
	// module to import their package, but nothing is written to their tree.
	virtual := filepath.Join(dir, ".gowasm", "recorder", "main_gen.go")
	ov, err := overlay.New(map[string][]byte{virtual: src})
	if err != nil {
		return nil, err
	}
	defer ov.Close()

	records, err := runRecorder(dir, execWrapper, ov.Path)
	if err != nil {
		return nil, err
	}
	// Run it a second time and compare. A call whose result changes between two
	// identical runs -- a timestamp, a duration, a random value, map ordering --
	// cannot be asserted on, and is skipped rather than made into a flaky test.
	second, err := runRecorder(dir, execWrapper, ov.Path)
	if err != nil {
		return nil, err
	}
	if len(records) != total {
		return nil, fmt.Errorf("recorded %d fixture(s) for %d call(s)", len(records), total)
	}

	out := make([][]Fixture, len(groups))
	next := 0
	for gi, g := range groups {
		fixtures := make([]Fixture, 0, len(g.Calls))
		for _, c := range g.Calls {
			i := next
			next++
			r := records[i]
			f := Fixture{
				NonDeterministic: i < len(second) && !sameOutcome(r, second[i]),
				Example:          c.Example,
				JSFunc:           c.JSFunc,
				Pos:              c.Pos,
				Error:            r.Error,
				Void:             r.Void,
			}
			// The arguments come back re-encoded from their decoded Go values,
			// so every non-omitempty field is present and the literal is
			// guaranteed to satisfy the generated TypeScript interface.
			for _, a := range r.Args {
				f.Args = append(f.Args, string(a))
			}
			if len(r.Result) > 0 {
				f.Result = string(r.Result)
			}
			fixtures = append(fixtures, f)
		}
		out[gi] = fixtures
	}
	return out, nil
}

// runRecorder executes the generated program once and decodes what it printed.
func runRecorder(dir, execWrapper, overlayPath string) ([]record, error) {
	args := []string{"run", "-overlay", overlayPath}
	env := os.Environ()
	if execWrapper != "" {
		// The explicit -exec form: cmd/go does not add lib/wasm to PATH, so a
		// stale wrapper from an older Go would otherwise be found first.
		args = append(args, "-exec="+execWrapper)
		env = append(env, "GOOS=js", "GOARCH=wasm")
	}
	args = append(args, "./.gowasm/recorder")

	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("recording example fixtures: %w\n%s", err, stderr.String())
	}

	var records []record
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		return nil, fmt.Errorf("decoding recorded fixtures: %w", err)
	}
	return records, nil
}

// sameOutcome reports whether two recordings of the same call agree.
func sameOutcome(a, b record) bool {
	return string(a.Result) == string(b.Result) && a.Error == b.Error && a.Void == b.Void
}

func renderRecorder(groups []Group) ([]byte, error) {
	imp := newImports()

	// Every package is declared before anything is rendered: one may take a type
	// declared in another, and the qualifier has to know its alias by the time it
	// sees that type. Declaring does not import, because a package whose Example
	// functions produced no recordable call contributes no code, and an unused
	// import does not compile.
	aliases := make([]string, len(groups))
	for i, g := range groups {
		aliases[i] = imp.declare(g.Module)
	}

	var body strings.Builder
	for gi, g := range groups {
		if len(g.Calls) == 0 {
			continue
		}
		imp.addAlias(aliases[gi], g.Module.PkgPath)
		byName := map[string]scan.Func{}
		for _, f := range g.Module.Funcs {
			byName[f.GoName] = f
		}
		for _, c := range g.Calls {
			f, ok := byName[c.GoFunc]
			if !ok {
				return nil, fmt.Errorf("%s: unknown function %s", c.Pos, c.GoFunc)
			}
			block, err := renderCall(c, f, aliases[gi], imp)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", c.Pos, err)
			}
			body.WriteString(block)
		}
	}

	var buf bytes.Buffer
	buf.WriteString("// Code generated by gowasm. DO NOT EDIT.\n\npackage main\n\nimport (\n")
	for _, spec := range imp.specs() {
		if spec.alias != "" {
			fmt.Fprintf(&buf, "\t%s %q\n", spec.alias, spec.path)
		} else {
			fmt.Fprintf(&buf, "\t%q\n", spec.path)
		}
	}
	buf.WriteString(")\n\n")
	buf.WriteString(recorderPreamble)
	buf.WriteString("func main() {\n\tout := make([]record, 0)\n")
	buf.WriteString(body.String())
	buf.WriteString("\tenc := json.NewEncoder(os.Stdout)\n\tif err := enc.Encode(out); err != nil {\n\t\tpanic(err)\n\t}\n}\n")

	src, err := format.Source(buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("generated recorder is not valid Go: %w\n%s", err, buf.String())
	}
	return src, nil
}

const recorderPreamble = `type record struct {
	Args   []json.RawMessage ` + "`json:\"args\"`" + `
	Result json.RawMessage   ` + "`json:\"result,omitempty\"`" + `
	Error  string            ` + "`json:\"error,omitempty\"`" + `
	Void   bool              ` + "`json:\"void,omitempty\"`" + `
}

// reencode renders a decoded argument back to JSON, so the recorded literal has
// every field the generated TypeScript interface requires.
func reencode(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

`

func renderCall(c scan.ExampleCall, f scan.Func, alias string, imp *imports) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "\tout = append(out, func() (r record) {\n")
	// A panic in the user's code should fail one fixture, not the whole run.
	b.WriteString("\t\tdefer func() {\n\t\t\tif p := recover(); p != nil {\n\t\t\t\tr.Error = fmt.Sprintf(\"panic: %v\", p)\n\t\t\t}\n\t\t}()\n")

	args := make([]string, 0, len(f.Params)+1)
	if f.HasContext {
		imp.add("context")
		args = append(args, "context.Background()")
	}

	for j, p := range f.Params {
		goType := types.TypeString(p.Type, imp.qualifier)
		fmt.Fprintf(&b, "\t\tvar p%d %s\n", j, goType)
		fmt.Fprintf(&b, "\t\tif err := json.Unmarshal([]byte(%s), &p%d); err != nil {\n", backquote(c.Args[j]), j)
		fmt.Fprintf(&b, "\t\t\tr.Error = err.Error()\n\t\t\treturn r\n\t\t}\n")
		arg := fmt.Sprintf("p%d", j)
		if f.Variadic && j == len(f.Params)-1 {
			arg += "..."
		}
		args = append(args, arg)
	}

	for j := range f.Params {
		fmt.Fprintf(&b, "\t\tr.Args = append(r.Args, reencode(p%d))\n", j)
	}

	call := fmt.Sprintf("%s.%s(%s)", alias, f.GoName, strings.Join(args, ", "))

	n := len(f.Results)
	switch {
	case n == 0 && !f.ReturnsError:
		fmt.Fprintf(&b, "\t\t%s\n\t\tr.Void = true\n", call)
	case n == 0 && f.ReturnsError:
		fmt.Fprintf(&b, "\t\tif err := %s; err != nil {\n\t\t\tr.Error = err.Error()\n\t\t\treturn r\n\t\t}\n\t\tr.Void = true\n", call)
	case n == 1 && !f.ReturnsError:
		fmt.Fprintf(&b, "\t\tr.Result = reencode(%s)\n", call)
	case n == 1 && f.ReturnsError:
		fmt.Fprintf(&b, "\t\tv, err := %s\n\t\tif err != nil {\n\t\t\tr.Error = err.Error()\n\t\t\treturn r\n\t\t}\n\t\tr.Result = reencode(v)\n", call)
	default:
		names := make([]string, 0, n)
		for j := 0; j < n; j++ {
			names = append(names, fmt.Sprintf("v%d", j))
		}
		joined := strings.Join(names, ", ")
		if f.ReturnsError {
			fmt.Fprintf(&b, "\t\t%s, err := %s\n\t\tif err != nil {\n\t\t\tr.Error = err.Error()\n\t\t\treturn r\n\t\t}\n", joined, call)
		} else {
			fmt.Fprintf(&b, "\t\t%s := %s\n", joined, call)
		}
		fmt.Fprintf(&b, "\t\tr.Result = reencode([]any{%s})\n", joined)
	}

	b.WriteString("\t\treturn r\n\t}())\n")
	return b.String(), nil
}

// backquote renders a JSON literal as a Go raw string, falling back to a quoted
// string when the literal itself contains a backquote.
func backquote(s string) string {
	if !strings.Contains(s, "`") {
		return "`" + s + "`"
	}
	return fmt.Sprintf("%q", s)
}

// --- import bookkeeping ---

type importSpec struct{ alias, path string }

type imports struct {
	userAliases map[string]string
	byPath      map[string]string
}

func newImports() *imports {
	i := &imports{userAliases: map[string]string{}, byPath: map[string]string{}}
	i.add("encoding/json")
	i.add("fmt")
	i.add("os")
	return i
}

// declare names a recorded package without importing it, so the qualifier can
// resolve its types while the import block still only lists what is used.
//
// A lone package keeps the name userpkg, so single-package output does not
// change.
func (i *imports) declare(mod *scan.Module) string {
	alias := "userpkg"
	if mod.Namespace != "" {
		alias = "pkg_" + mod.Namespace
	}
	i.userAliases[mod.PkgPath] = alias
	return alias
}

func (i *imports) add(path string)             { i.addAlias("", path) }
func (i *imports) addAlias(alias, path string) { i.byPath[path] = alias }

func (i *imports) qualifier(p *types.Package) string {
	if p == nil {
		return ""
	}
	if alias, exposed := i.userAliases[p.Path()]; exposed {
		i.addAlias(alias, p.Path())
		return alias
	}
	i.add(p.Path())
	return p.Name()
}

func (i *imports) specs() []importSpec {
	paths := make([]string, 0, len(i.byPath))
	for p := range i.byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]importSpec, 0, len(paths))
	for _, p := range paths {
		out = append(out, importSpec{alias: i.byPath[p], path: p})
	}
	return out
}
