package tsmap

import (
	"fmt"
	"go/types"
	"strings"
)

// Int64Mode selects how 64-bit integers cross the boundary.
type Int64Mode string

const (
	// Int64Number maps int64/uint64 to `number`. JSON-native, but values
	// beyond 2^53 lose precision.
	Int64Number Int64Mode = "number"
	// Int64String maps them to `string`, preserving every value. Requires the
	// Go field to carry a `,string` json tag option to match.
	Int64String Int64Mode = "string"
)

// Built-in names the generated TypeScript may reference.
const (
	// Bytes is how Go []byte appears in TypeScript. encoding/json moves the
	// data base64-encoded; the generated client converts at the boundary so the
	// public API is a real typed array.
	Bytes       = "Uint8Array"
	ISODateTime = "ISODateTime"
)

// Mapper converts Go types to TypeScript, accumulating the named types it
// encounters along the way.
//
// Discovery is a side effect of mapping: walking a function signature registers
// every struct and named scalar it touches, so there is no separate traversal
// to keep in sync.
type Mapper struct {
	Int64 Int64Mode

	named map[string]*types.Named
	order []string
	used  map[string]bool
}

func New(mode Int64Mode) *Mapper {
	if mode == "" {
		mode = Int64Number
	}
	return &Mapper{
		Int64: mode,
		named: map[string]*types.Named{},
		used:  map[string]bool{},
	}
}

// Discovered returns the named types encountered so far, in first-seen order.
func (m *Mapper) Discovered() []*types.Named {
	out := make([]*types.Named, 0, len(m.order))
	for _, n := range m.order {
		out = append(out, m.named[n])
	}
	return out
}

// UsesBytes and UsesISODateTime report whether these are actually referenced,
// so nothing unused is emitted.
func (m *Mapper) UsesBytes() bool       { return m.used[Bytes] }
func (m *Mapper) UsesISODateTime() bool { return m.used[ISODateTime] }

func (m *Mapper) record(n *types.Named) {
	name := n.Obj().Name()
	if _, seen := m.named[name]; !seen {
		m.named[name] = n
		m.order = append(m.order, name)
	}
}

// TS renders t as a TypeScript type expression.
//
// Types with no honest TypeScript equivalent are a hard error rather than a
// silent `unknown`: emitting `unknown` for a channel produces code that
// type-checks and then fails at runtime, which is strictly worse than refusing
// to generate.
func (m *Mapper) TS(t types.Type) (string, error) {
	switch u := types.Unalias(t).(type) {
	case *types.Basic:
		return m.basic(u)

	case *types.Pointer:
		inner, err := m.TS(u.Elem())
		if err != nil {
			return "", err
		}
		// Parenthesize unions so `*[]T` reads as `(T[]) | null`, not `T[] | null`
		// where the union binds loosely.
		if strings.Contains(inner, "|") {
			inner = "(" + inner + ")"
		}
		return inner + " | null", nil

	case *types.Slice:
		if isByte(u.Elem()) {
			m.used[Bytes] = true
			return Bytes, nil
		}
		return m.elemArray(u.Elem())

	case *types.Array:
		if isByte(u.Elem()) {
			m.used[Bytes] = true
			return Bytes, nil
		}
		return m.elemArray(u.Elem())

	case *types.Map:
		return m.mapType(u)

	case *types.Named:
		return m.named_(u)

	case *types.Interface:
		// Both `any` and a method-bearing interface land here. Neither has a
		// JSON shape we can know statically.
		return "unknown", nil

	case *types.Struct:
		return "", fmt.Errorf("anonymous struct types are not supported; give it a named type")

	case *types.Chan:
		return "", fmt.Errorf("channel types cannot cross the JavaScript boundary")

	case *types.Signature:
		return "", fmt.Errorf("function types cannot cross the JavaScript boundary")

	case *types.TypeParam:
		return "", fmt.Errorf("generic type parameters are not supported")

	case *types.Tuple:
		return "", fmt.Errorf("tuple types are not supported")

	default:
		return "", fmt.Errorf("unsupported type %s", t.String())
	}
}

func (m *Mapper) elemArray(elem types.Type) (string, error) {
	inner, err := m.TS(elem)
	if err != nil {
		return "", err
	}
	if strings.Contains(inner, "|") {
		inner = "(" + inner + ")"
	}
	return inner + "[]", nil
}

func (m *Mapper) mapType(u *types.Map) (string, error) {
	// JSON object keys are always strings. encoding/json accepts string kinds,
	// integer kinds and encoding.TextMarshaler as map keys; anything else fails
	// at runtime, so reject it here where the message can point at the type.
	switch k := types.Unalias(u.Key().Underlying()).(type) {
	case *types.Basic:
		if k.Info()&(types.IsString|types.IsInteger) == 0 {
			return "", fmt.Errorf("map key type %s is not encodable as a JSON object key", u.Key())
		}
	default:
		return "", fmt.Errorf("map key type %s is not encodable as a JSON object key", u.Key())
	}

	val, err := m.TS(u.Elem())
	if err != nil {
		return "", err
	}
	// Always `string` on the key side: even map[int]T serializes with string
	// keys, so Record<number, T> would be a lie.
	return "Record<string, " + val + ">", nil
}

func (m *Mapper) named_(u *types.Named) (string, error) {
	obj := u.Obj()

	if obj.Pkg() != nil && obj.Pkg().Path() == "time" && obj.Name() == "Time" {
		m.used[ISODateTime] = true
		return ISODateTime, nil
	}
	if obj.Pkg() == nil && obj.Name() == "error" {
		return "string", nil
	}
	// A named type implementing json.Marshaler has a shape we cannot infer;
	// time.Time above is the one case we special-case by hand.
	if u.TypeArgs() != nil && u.TypeArgs().Len() > 0 {
		return "", fmt.Errorf("generic type %s is not supported", obj.Name())
	}

	switch under := u.Underlying().(type) {
	case *types.Struct:
		m.record(u)
		return obj.Name(), nil
	case *types.Basic:
		m.record(u)
		return obj.Name(), nil
	case *types.Interface:
		return "unknown", nil
	default:
		// Named slice/map/pointer: emitted as a TS alias to the underlying
		// shape, keeping the name meaningful in signatures.
		_ = under
		m.record(u)
		return obj.Name(), nil
	}
}

func (m *Mapper) basic(b *types.Basic) (string, error) {
	switch b.Kind() {
	case types.Bool, types.UntypedBool:
		return "boolean", nil
	case types.String, types.UntypedString:
		return "string", nil
	case types.Int64, types.Uint64:
		if m.Int64 == Int64String {
			return "string", nil
		}
		return "number", nil
	case types.Complex64, types.Complex128:
		return "", fmt.Errorf("complex numbers have no JSON representation")
	case types.UnsafePointer, types.Uintptr:
		return "", fmt.Errorf("%s cannot cross the JavaScript boundary", b.Name())
	}

	info := b.Info()
	switch {
	case info&types.IsInteger != 0, info&types.IsFloat != 0:
		return "number", nil
	}
	return "", fmt.Errorf("unsupported basic type %s", b.Name())
}

// IsInt64 reports whether t is a 64-bit integer, so callers can attach a
// precision warning to the emitted declaration.
func IsInt64(t types.Type) bool {
	b, ok := types.Unalias(t.Underlying()).(*types.Basic)
	if !ok {
		return false
	}
	return b.Kind() == types.Int64 || b.Kind() == types.Uint64
}

func isByte(t types.Type) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	return ok && b.Kind() == types.Uint8
}
