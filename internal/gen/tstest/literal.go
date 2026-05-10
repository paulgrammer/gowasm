package tstest

import (
	"encoding/json"
	"fmt"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/paulgrammer/gowasm/internal/scan"
)

// literalizer renders recorded JSON as TypeScript source.
//
// A straight copy of the JSON would not type-check: binary data is recorded
// base64-encoded, the way encoding/json writes it, but the generated API
// exposes it as a typed array. Walking the value alongside its Go type puts the
// conversion exactly where the bytes are, however deeply nested.
type literalizer struct {
	structs map[string]scan.Struct
	// usesBytes records whether the helper needs emitting.
	usesBytes bool
}

func newLiteralizer(mod *scan.Module) *literalizer {
	l := &literalizer{structs: map[string]scan.Struct{}}
	for _, s := range mod.Structs {
		l.structs[s.Name] = s
	}
	return l
}

// render converts one recorded JSON value to a TypeScript expression.
func (l *literalizer) render(t types.Type, raw string) string {
	if raw == "" {
		return "undefined"
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		// Not decodable: emit it verbatim rather than losing the fixture.
		return raw
	}
	return l.walk(t, value)
}

func (l *literalizer) walk(t types.Type, v any) string {
	if v == nil {
		// A nil Go slice or map records as null, but the generated signature
		// says T[], Uint8Array or Record<...>. All three decode to the same
		// thing on the Go side, so the fixture uses the form the type admits.
		switch u := types.Unalias(t).(type) {
		case *types.Slice:
			if isByteElem(u.Elem()) {
				return l.bytes("")
			}
			return "[]"
		case *types.Array:
			if isByteElem(u.Elem()) {
				return l.bytes("")
			}
			return "[]"
		case *types.Map:
			return "{}"
		}
		return "null"
	}
	if t == nil {
		return jsonValue(v)
	}

	switch u := types.Unalias(t).(type) {
	case *types.Slice:
		if isByteElem(u.Elem()) {
			return l.bytes(v)
		}
		return l.walkList(u.Elem(), v)

	case *types.Array:
		if isByteElem(u.Elem()) {
			return l.bytes(v)
		}
		return l.walkList(u.Elem(), v)

	case *types.Map:
		obj, ok := v.(map[string]any)
		if !ok {
			return jsonValue(v)
		}
		keys := sortedKeys(obj)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s: %s", tsKey(k), l.walk(u.Elem(), obj[k])))
		}
		return "{ " + strings.Join(parts, ", ") + " }"

	case *types.Pointer:
		return l.walk(u.Elem(), v)

	case *types.Named:
		if s, known := l.structs[u.Obj().Name()]; known {
			return l.walkStruct(s, v)
		}
		return l.walk(u.Underlying(), v)

	default:
		return jsonValue(v)
	}
}

func (l *literalizer) walkList(elem types.Type, v any) string {
	items, ok := v.([]any)
	if !ok {
		return jsonValue(v)
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, l.walk(elem, item))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func (l *literalizer) walkStruct(s scan.Struct, v any) string {
	obj, ok := v.(map[string]any)
	if !ok {
		return jsonValue(v)
	}
	byName := map[string]types.Type{}
	for _, f := range s.Fields {
		byName[f.JSName] = f.Type
	}

	keys := sortedKeys(obj)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s: %s", tsKey(k), l.walk(byName[k], obj[k])))
	}
	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// bytes renders base64-encoded binary as a typed array.
func (l *literalizer) bytes(v any) string {
	s, ok := v.(string)
	if !ok {
		return jsonValue(v)
	}
	l.usesBytes = true
	return fmt.Sprintf("bytes(%s)", strconv.Quote(s))
}

// jsonValue renders a decoded JSON value as TypeScript. JSON is a subset of
// TypeScript expression syntax, so this is mostly a faithful re-encoding, with
// object keys quoted only when they are not valid identifiers.
func jsonValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return formatNumber(t)
	case string:
		return strconv.Quote(t)
	case []any:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, jsonValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := sortedKeys(t)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s: %s", tsKey(k), jsonValue(t[k])))
		}
		if len(parts) == 0 {
			return "{}"
		}
		return "{ " + strings.Join(parts, ", ") + " }"
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "null"
		}
		return string(b)
	}
}

func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// tsKey quotes an object key unless it is a plain identifier.
func tsKey(k string) string {
	if k == "" {
		return strconv.Quote(k)
	}
	for i, r := range k {
		ok := r == '_' || r == '$' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9')
		if !ok {
			return strconv.Quote(k)
		}
	}
	return k
}

func isByteElem(t types.Type) bool {
	b, ok := types.Unalias(t).(*types.Basic)
	return ok && b.Kind() == types.Uint8
}
