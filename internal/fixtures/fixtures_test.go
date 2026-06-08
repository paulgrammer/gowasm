package fixtures

import (
	"strings"
	"testing"

	"github.com/paulgrammer/gowasm/internal/scan"
)

// A package whose Example functions produced nothing recordable contributes no
// code to the recorder, so importing it would not compile. This is not
// hypothetical: a project with one documented package and one still being
// written is the normal way a second package starts life.
func TestRecorderSkipsAPackageWithNoCalls(t *testing.T) {
	quiet := &scan.Module{
		Namespace: "store",
		PkgPath:   "example.com/p/pkg/store",
		PkgName:   "store",
		Funcs:     []scan.Func{{GoName: "Get", JSName: "get"}},
	}
	busy := &scan.Module{
		Namespace: "text",
		PkgPath:   "example.com/p/text",
		PkgName:   "text",
		Funcs:     []scan.Func{{GoName: "Ping", JSName: "ping"}},
	}

	src, err := renderRecorder([]Group{
		{Module: busy, Calls: []scan.ExampleCall{{Example: "ExamplePing", GoFunc: "Ping", JSFunc: "ping"}}},
		{Module: quiet}, // documented by nothing gowasm can replay
	})
	if err != nil {
		t.Fatalf("rendering the recorder: %v", err)
	}
	got := string(src)

	if strings.Contains(got, quiet.PkgPath) {
		t.Errorf("the recorder imports %s but never calls into it:\n%s", quiet.PkgPath, got)
	}
	if !strings.Contains(got, busy.PkgPath) {
		t.Errorf("the recorder should import %s, which it does call:\n%s", busy.PkgPath, got)
	}
	if !strings.Contains(got, "pkg_text.Ping(") {
		t.Errorf("the call should be qualified by the package's alias:\n%s", got)
	}
}

// Every package keeps its own alias, so two packages exporting the same
// function name stay distinguishable in the generated recorder.
func TestRecorderQualifiesEachPackage(t *testing.T) {
	mk := func(ns, path string) *scan.Module {
		return &scan.Module{
			Namespace: ns, PkgPath: path, PkgName: ns,
			Funcs: []scan.Func{{GoName: "New", JSName: "new"}},
		}
	}
	call := func(pkg string) scan.ExampleCall {
		return scan.ExampleCall{Example: "ExampleNew", GoFunc: "New", JSFunc: "new", Pos: pkg}
	}

	src, err := renderRecorder([]Group{
		{Module: mk("text", "example.com/p/text"), Calls: []scan.ExampleCall{call("text")}},
		{Module: mk("store", "example.com/p/store"), Calls: []scan.ExampleCall{call("store")}},
	})
	if err != nil {
		t.Fatalf("rendering the recorder: %v", err)
	}
	for _, want := range []string{"pkg_text.New(", "pkg_store.New("} {
		if !strings.Contains(string(src), want) {
			t.Errorf("expected %s in:\n%s", want, src)
		}
	}
}
