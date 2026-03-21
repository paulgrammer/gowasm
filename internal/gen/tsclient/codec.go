package tsclient

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"github.com/paulgrammer/gowasm/internal/scan"
	"github.com/paulgrammer/gowasm/internal/tsmap"
)

// codec works out where binary data sits inside a type and emits the
// TypeScript that converts it at the boundary.
//
// encoding/json moves a Go []byte as a base64 string, but a JavaScript caller
// wants a typed array. Rather than leak that encoding into the public API, the
// generated client converts on the way in and out. The conversion has to follow
// the shape of the data, so a []byte nested inside a struct inside a slice is
// found and converted too.
type codec struct {
	// needs caches, per named type, whether it contains binary data anywhere.
	needs map[string]bool
	// structs maps a named type to its declaration, for walking fields.
	structs map[string]scan.Struct
}

func newCodec(mod *scan.Module) *codec {
	c := &codec{needs: map[string]bool{}, structs: map[string]scan.Struct{}}
	for _, s := range mod.Structs {
		c.structs[s.Name] = s
	}
	return c
}

// Needed reports whether anything in the module requires conversion.
func (c *codec) Needed(mod *scan.Module) bool {
	for _, f := range mod.Funcs {
		for _, p := range f.Params {
			if c.needsCodec(p.Type, nil) {
				return true
			}
		}
		for _, r := range f.Results {
			if c.needsCodec(r.Type, nil) {
				return true
			}
		}
	}
	return false
}

// Structs lists the named struct types that need a conversion function, in a
// stable order.
func (c *codec) Structs(mod *scan.Module) []scan.Struct {
	var out []scan.Struct
	for _, s := range mod.Structs {
		if s.Named != nil && c.needsCodec(s.Named, nil) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// needsCodec reports whether values of t carry binary data. The seen set breaks
// cycles in self-referential types.
func (c *codec) needsCodec(t types.Type, seen map[string]bool) bool {
	if t == nil {
		return false
	}
	switch u := types.Unalias(t).(type) {
	case *types.Slice:
		if isByteElem(u.Elem()) {
			return true
		}
		return c.needsCodec(u.Elem(), seen)
	case *types.Array:
		if isByteElem(u.Elem()) {
			return true
		}
		return c.needsCodec(u.Elem(), seen)
	case *types.Map:
		return c.needsCodec(u.Elem(), seen)
	case *types.Pointer:
		return c.needsCodec(u.Elem(), seen)
	case *types.Named:
		name := u.Obj().Name()
		if seen == nil {
			seen = map[string]bool{}
		}
		if seen[name] {
			return false
		}
		seen[name] = true
		if cached, ok := c.needs[name]; ok {
			return cached
		}
		s, known := c.structs[name]
		if !known {
			return c.needsCodec(u.Underlying(), seen)
		}
		result := false
		for _, f := range s.Fields {
			if c.needsCodec(f.Type, seen) {
				result = true
				break
			}
		}
		c.needs[name] = result
		return result
	default:
		return false
	}
}

// decode renders the expression converting a wire value in expr into the public
// type, or returns expr unchanged when no conversion is needed.
func (c *codec) decode(t types.Type, expr string) string {
	return c.convert(t, expr, "b64ToBytes", "decode")
}

// encode is decode's inverse, for values on their way to Go.
func (c *codec) encode(t types.Type, expr string) string {
	return c.convert(t, expr, "bytesToB64", "encode")
}

func (c *codec) convert(t types.Type, expr, leaf, prefix string) string {
	if !c.needsCodec(t, nil) {
		return expr
	}
	switch u := types.Unalias(t).(type) {
	case *types.Slice:
		if isByteElem(u.Elem()) {
			return fmt.Sprintf("%s(%s)", leaf, expr)
		}
		return fmt.Sprintf("%s.map((v: any) => %s)", expr, c.convert(u.Elem(), "v", leaf, prefix))
	case *types.Array:
		if isByteElem(u.Elem()) {
			return fmt.Sprintf("%s(%s)", leaf, expr)
		}
		return fmt.Sprintf("%s.map((v: any) => %s)", expr, c.convert(u.Elem(), "v", leaf, prefix))
	case *types.Map:
		return fmt.Sprintf("mapValues(%s, (v: any) => %s)", expr, c.convert(u.Elem(), "v", leaf, prefix))
	case *types.Pointer:
		return fmt.Sprintf("(%s == null ? null : %s)", expr, c.convert(u.Elem(), expr, leaf, prefix))
	case *types.Named:
		if _, known := c.structs[u.Obj().Name()]; known {
			return fmt.Sprintf("%s%s(%s)", prefix, u.Obj().Name(), expr)
		}
		return c.convert(u.Underlying(), expr, leaf, prefix)
	default:
		return expr
	}
}

// structBody renders the object literal for one struct's conversion function.
func (c *codec) structBody(s scan.Struct, leaf, prefix string) string {
	var parts []string
	for _, f := range s.Fields {
		if !c.needsCodec(f.Type, nil) {
			continue
		}
		access := fmt.Sprintf("v.%s", f.JSName)
		converted := c.convert(f.Type, access, leaf, prefix)
		if f.Optional {
			// An omitted field must stay omitted rather than becoming null.
			converted = fmt.Sprintf("%s === undefined ? undefined : %s", access, converted)
		}
		parts = append(parts, fmt.Sprintf("    %s: %s,", f.JSName, converted))
	}
	if len(parts) == 0 {
		return "  return v;"
	}
	return "  return {\n    ...v,\n" + strings.Join(parts, "\n") + "\n  };"
}

func isByteElem(t types.Type) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	return ok && b.Kind() == types.Uint8
}

// codecView is what the codec template renders.
type codecView struct {
	Name        string
	DecodeBody  string
	EncodeBody  string
	UsesMapValu bool
}

func buildCodecViews(c *codec, mod *scan.Module) ([]codecView, bool) {
	var out []codecView
	usesMap := false
	for _, s := range c.Structs(mod) {
		dv := c.structBody(s, "b64ToBytes", "decode")
		ev := c.structBody(s, "bytesToB64", "encode")
		if strings.Contains(dv, "mapValues(") || strings.Contains(ev, "mapValues(") {
			usesMap = true
		}
		out = append(out, codecView{Name: s.Name, DecodeBody: dv, EncodeBody: ev})
	}
	// A top-level map of binary values also needs the helper.
	for _, f := range mod.Funcs {
		for _, p := range f.Params {
			if strings.Contains(c.encode(p.Type, "x"), "mapValues(") {
				usesMap = true
			}
		}
		for _, r := range f.Results {
			if strings.Contains(c.decode(r.Type, "x"), "mapValues(") {
				usesMap = true
			}
		}
	}
	return out, usesMap
}

// bytesAlias reports whether the module references the typed-array type at all.
func bytesAlias(mod *scan.Module) bool { return mod.UsesBytes }

var _ = tsmap.Bytes
