package tsclient

import (
	"fmt"
	"go/types"
	"sort"
	"strings"

	"github.com/paulgrammer/gowasm/internal/scan"
)

// classSet identifies the named types that cross as handles, so a *T can be
// wrapped on the way out and unwrapped on the way in.
type classSet map[*types.TypeName]string

func newClassSet(mod *scan.Module) classSet {
	cs := classSet{}
	for _, c := range mod.Classes {
		if c.Named != nil {
			cs[c.Named.Obj()] = c.Name
		}
	}
	return cs
}

// of reports the class a *T refers to.
func (cs classSet) of(t types.Type) (string, bool) {
	p, ok := types.Unalias(t).(*types.Pointer)
	if !ok {
		return "", false
	}
	n, ok := types.Unalias(p.Elem()).(*types.Named)
	if !ok {
		return "", false
	}
	name, isClass := cs[n.Obj()]
	return name, isClass
}

// has reports whether the type mentions a class anywhere, which is how the
// codec and the fixture literalizer are kept away from them.
func (cs classSet) has(t types.Type) bool {
	switch u := types.Unalias(t).(type) {
	case *types.Pointer:
		if _, ok := cs.of(u); ok {
			return true
		}
		return cs.has(u.Elem())
	case *types.Slice:
		return cs.has(u.Elem())
	case *types.Array:
		return cs.has(u.Elem())
	case *types.Map:
		return cs.has(u.Elem())
	case *types.Named:
		_, ok := cs[u.Obj()]
		return ok
	}
	return false
}

// --- view models ---

type accessorView struct {
	GoName  string
	JSName  string
	SetName string
	Doc     string
	TS      string
	IsInt64 bool
	At      string
	SetAt   string
	WireGet string
	WireSet string
	GetExpr string
	SetArg  string
}

type methodView struct {
	Key      string
	JSName   string
	Doc      string
	TSParams string
	TSReturn string
	At       string
	CallExpr string
}

type classView struct {
	Name        string
	GoType      string
	Doc         string
	HasGoClose  bool
	WireGoClose string
	Fields      []accessorView
	Methods     []methodView
}

// buildClasses renders every class in a module.
func buildClasses(mod *scan.Module, cdc *codec, cs classSet) []classView {
	out := make([]classView, 0, len(mod.Classes))
	for _, c := range mod.Classes {
		v := classView{
			Name:       c.Name,
			GoType:     "*" + mod.PkgName + "." + c.Name,
			Doc:        c.Doc,
			HasGoClose: c.HasGoClose,
		}
		if c.HasGoClose {
			v.WireGoClose = mod.Wire(scan.Func{Recv: c.Name, JSName: "__goClose"})
		}

		for _, a := range c.Fields {
			get, set := mod.WireAccessor(c, a)
			view := accessorView{
				GoName:  a.GoName,
				JSName:  a.JSName,
				SetName: a.SetName,
				Doc:     a.Doc,
				TS:      a.TS,
				IsInt64: a.IsInt64,
				At:      c.Name + "." + a.JSName,
				SetAt:   c.Name + "." + a.SetName,
				WireGet: get,
				WireSet: set,
				SetArg:  cdc.encode(a.Type, "value"),
			}
			raw := fmt.Sprintf("this.#rt.call<%s>(%q, [h])", a.TS, get)
			awaited := fmt.Sprintf("(await this.#rt.call<any>(%q, [h]))", get)
			if decoded := cdc.decode(a.Type, awaited); decoded != awaited {
				raw = decoded
			}
			view.GetExpr = raw
			v.Fields = append(v.Fields, view)
		}

		for _, m := range c.Methods {
			v.Methods = append(v.Methods, buildMethod(mod, c, m, cdc, cs))
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func buildMethod(mod *scan.Module, c scan.Class, f scan.Func, cdc *codec, cs classSet) methodView {
	at := c.Name + "." + f.JSName
	params, args := signatureParts(f, cdc, cs, "this.#rt", at)

	call := append([]string{"h"}, args...)
	wire := mod.Wire(f)

	v := methodView{
		Key:      memberKey(f.JSName),
		JSName:   f.JSName,
		Doc:      f.Doc,
		TSParams: strings.Join(params, ", "),
		TSReturn: f.TSReturn(),
		At:       at,
	}
	v.CallExpr = resultExpr(f, cdc, cs, "this.#rt", wire, call)
	return v
}

// signatureParts renders the TypeScript parameter list and the matching
// argument expressions, converting a class argument to its handle.
func signatureParts(f scan.Func, cdc *codec, cs classSet, rt, at string) (params, args []string) {
	for i, p := range f.Params {
		last := i == len(f.Params)-1
		if f.Variadic && last {
			params = append(params, "..."+p.JSName+": "+arrayOf(p.TS))
		} else {
			params = append(params, p.JSName+": "+p.TS)
		}
		if name, isClass := cs.of(p.Type); isClass {
			args = append(args, fmt.Sprintf("%s.__unwrap(%s, %s, %q)", name, p.JSName, rt, at))
			continue
		}
		args = append(args, cdc.encode(p.Type, p.JSName))
	}
	return params, args
}

// resultExpr renders the awaited call, wrapping a returned handle back into its
// class and decoding binary data.
func resultExpr(f scan.Func, cdc *codec, cs classSet, rt, wire string, args []string) string {
	list := strings.Join(args, ", ")
	if len(f.Results) == 1 {
		if name, isClass := cs.of(f.Results[0].Type); isClass {
			return fmt.Sprintf("%s.__wrap(%s, await %s.call<number>(%q, [%s]))", name, rt, rt, wire, list)
		}
		awaited := fmt.Sprintf("(await %s.call<any>(%q, [%s]))", rt, wire, list)
		if decoded := cdc.decode(f.Results[0].Type, awaited); decoded != awaited {
			return decoded
		}
	}
	return fmt.Sprintf("%s.call<%s>(%q, [%s])", rt, f.TSReturn(), wire, list)
}

// classNames lists the classes a module declares, for imports and re-exports.
func classNames(mod *scan.Module) []string {
	out := make([]string, 0, len(mod.Classes))
	for _, c := range mod.Classes {
		out = append(out, c.Name)
	}
	sort.Strings(out)
	return out
}
