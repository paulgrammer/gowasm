package scan

import (
	"strings"
	"testing"

	"github.com/paulgrammer/gowasm/internal/tsmap"
)

func loadClassify(t *testing.T, dir string) (*Module, error) {
	t.Helper()
	return Load(Options{Dir: "testdata/classify/" + dir, Pattern: ".", Int64: tsmap.Int64Number})
}

func mustLoad(t *testing.T, dir string) *Module {
	t.Helper()
	mod, err := loadClassify(t, dir)
	if err != nil {
		t.Fatalf("loading %s: %v", dir, err)
	}
	return mod
}

func classNames(mod *Module) []string {
	out := make([]string, 0, len(mod.Classes))
	for _, c := range mod.Classes {
		out = append(out, c.Name)
	}
	return out
}

func structNames(mod *Module) []string {
	out := make([]string, 0, len(mod.Structs))
	for _, s := range mod.Structs {
		out = append(out, s.Name)
	}
	return out
}

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// The clean case, and the distinction the whole feature rests on: one type in
// the package becomes a class, the other stays data.
func TestMethodsBehindAPointerBecomeAClass(t *testing.T) {
	mod := mustLoad(t, "handle")

	if got := classNames(mod); len(got) != 1 || got[0] != "Store" {
		t.Fatalf("classes = %v, want [Store]", got)
	}
	if !has(structNames(mod), "Snapshot") {
		t.Errorf("Snapshot has no methods and is used by value; it must stay an interface, got structs %v", structNames(mod))
	}
	if has(structNames(mod), "Store") {
		t.Error("Store is a class, so it must not also be declared as an interface")
	}

	c := mod.Classes[0]
	// A value receiver is a method like any other; it simply cannot mutate.
	for _, want := range []string{"get", "snapshot", "label"} {
		found := false
		for _, m := range c.Methods {
			if m.JSName == want {
				found = true
			}
		}
		if !found {
			t.Errorf("method %s is missing from the class", want)
		}
	}
	// Exported fields become accessors; unexported ones are invisible.
	if len(c.Fields) != 2 {
		t.Fatalf("fields = %d, want 2 (name, limit)", len(c.Fields))
	}
	if c.Fields[0].JSName != "name" || c.Fields[0].SetName != "setName" {
		t.Errorf("first accessor = %q/%q, want name/setName", c.Fields[0].JSName, c.Fields[0].SetName)
	}
}

// A debug String() is the most likely method anyone adds to a data type. If it
// alone flipped Point into an opaque handle, a harmless change would break
// every consumer of the published package.
func TestConventionalMethodsDoNotMakeAClass(t *testing.T) {
	mod := mustLoad(t, "stringer")
	if got := classNames(mod); len(got) != 0 {
		t.Errorf("classes = %v, want none: String() says nothing about identity", got)
	}
	if !has(structNames(mod), "Point") {
		t.Errorf("Point must stay an interface, got structs %v", structNames(mod))
	}
}

// The value use is reachable only through Store's own fields, so a lazy walk
// would decide Store is a handle and never look. See resource.go.
func TestAValueUseHiddenBehindFieldsIsStillFound(t *testing.T) {
	_, err := loadClassify(t, "cycle")
	if err == nil {
		t.Fatal("Store is used both ways; that should not generate")
	}
	for _, want := range []string{"as a pointer", "by value", "gowasm:data"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should mention %q, got: %v", want, err)
		}
	}
}

// No ambiguity here: it is only ever a value, so it stays data and its methods
// are reported as unreachable rather than dropped in silence.
func TestAValueOnlyTypeStaysDataWithANote(t *testing.T) {
	mod := mustLoad(t, "valueuse")
	if got := classNames(mod); len(got) != 0 {
		t.Errorf("classes = %v, want none", got)
	}
	joined := strings.Join(mod.Notes, "\n")
	if !strings.Contains(joined, "Amount") {
		t.Errorf("the scan should say Amount's methods are not exposed, got notes: %v", mod.Notes)
	}
}

// A promoted Lock/Unlock must not be callable: a lock() from JavaScript with no
// unlock() would wedge the instance for good.
func TestPromotedMethodsAreNotExposed(t *testing.T) {
	mod := mustLoad(t, "embedded")
	if got := classNames(mod); len(got) != 1 {
		t.Fatalf("classes = %v, want [Store]", got)
	}
	for _, m := range mod.Classes[0].Methods {
		if m.JSName == "lock" || m.JSName == "unlock" {
			t.Errorf("promoted method %s must not be exposed", m.JSName)
		}
	}
}

// Close is the likeliest method on exactly the types this feature targets, so
// it is folded into the generated close() rather than colliding with it.
func TestGoCloseIsFoldedIntoTheGeneratedClose(t *testing.T) {
	mod := mustLoad(t, "closer")
	if len(mod.Classes) != 1 {
		t.Fatalf("classes = %v, want [File]", classNames(mod))
	}
	c := mod.Classes[0]
	if !c.HasGoClose {
		t.Error("Close() error should be recorded for folding")
	}
	for _, m := range c.Methods {
		if m.JSName == "close" {
			t.Error("Close must not also be emitted as an ordinary method")
		}
	}
}

func TestRefusedMembers(t *testing.T) {
	cases := map[string]string{
		"thenable":    "thenable",
		"setterclash": "setName",
	}
	for dir, want := range cases {
		_, err := loadClassify(t, dir)
		if err == nil {
			t.Errorf("%s should not generate", dir)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: the error should mention %q, got: %v", dir, want, err)
		}
	}
}

// A handle can only be a whole parameter or result. []*Store would put an
// integer inside a JSON array with nothing to say what it means.
func TestAPointerInsideASliceIsAValueUse(t *testing.T) {
	_, err := loadClassify(t, "sliceof")
	if err == nil {
		t.Fatal("Store is returned both as *Store and inside []*Store; that is ambiguous")
	}
	if !strings.Contains(err.Error(), "Store") {
		t.Errorf("the error should name Store, got: %v", err)
	}
}
