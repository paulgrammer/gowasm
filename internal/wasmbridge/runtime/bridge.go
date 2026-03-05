//go:build js && wasm

// Package runtime is the runtime half of gowasm. Generated code registers each
// exported Go function here, then calls Run to install them on a JS namespace
// object and block until the JS side disposes the instance.
//
// Every file in this directory is emitted into the package gowasm generates,
// rewritten to package main, so that generated code depends on nothing outside
// the user's own module. It is a real, compiled, tested package rather than a
// template: what is tested here is exactly what users run.
//
// The wire format is deliberately boring: one JSON string in (a JSON array of
// the call's arguments), one JSON string out (the marshalled result). syscall/js
// cannot pass Go structs directly — js.ValueOf panics on them, with no
// reflection fallback — so JSON is the only encoding that generalizes over
// arbitrary user types.
package runtime

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"syscall/js"
)

// Handler invokes one exported Go function. args holds the still-encoded call
// arguments, one element per declared parameter; generated code unmarshals each
// into its concrete type. The returned value is marshalled back to JSON.
type Handler func(args []json.RawMessage) (any, error)

type entry struct {
	name  string
	arity int
	fn    Handler
}

var registry = map[string]*entry{}

// Register records one exported function. Generated code calls this from main
// before Run. Registering the same name twice is a programming error in the
// generator, so it panics rather than silently shadowing.
func Register(name string, arity int, fn Handler) {
	if _, dup := registry[name]; dup {
		panic("wasmbridge: duplicate registration for " + name)
	}
	registry[name] = &entry{name: name, arity: arity, fn: fn}
}

var (
	promiseCtor = js.Global().Get("Promise")
	errorCtor   = js.Global().Get("Error")
	jsonObj     = js.Global().Get("JSON")
)

// errorf builds a real JS Error so callers get instanceof Error, a message and
// a stack. Rejecting with a bare string (as some bridges do) loses all of that.
func errorf(format string, a ...any) js.Value {
	e := errorCtor.New(fmt.Sprintf(format, a...))
	e.Set("name", "GoError")
	return e
}

func toJSError(err error) js.Value {
	e := errorCtor.New(err.Error())
	e.Set("name", "GoError")
	return e
}

// invoke runs one handler and returns the JSON-encoded result. A void function
// yields the empty string, which the TS side maps to undefined.
func (e *entry) invoke(raw string) (s string, err error) {
	// An unrecovered panic in wasm tears down the whole instance permanently:
	// every other exported function dies with it and there is no restart short
	// of re-instantiating the module. So every call is fenced.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s: panic: %v", e.name, r)
		}
	}()

	var args []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", fmt.Errorf("%s: malformed argument list: %w", e.name, err)
	}
	if len(args) != e.arity {
		return "", fmt.Errorf("%s: expected %d argument(s), got %d", e.name, e.arity, len(args))
	}

	result, err := e.fn(args)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}

	out, err := json.Marshal(normalized(result))
	if err != nil {
		return "", fmt.Errorf("%s: cannot encode result: %w", e.name, err)
	}
	return string(out), nil
}

// wrap adapts a handler into a JS function returning a Promise.
//
// The body must return promptly: a JS->Go call re-enters the Go scheduler
// synchronously, inside the JS call stack, so blocking here blocks the event
// loop and deadlocks anything that needs it. The actual work therefore runs on
// its own goroutine and reports back through resolve/reject.
func wrap(e *entry) js.Func {
	return js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].Type() != js.TypeString {
			// Thrown synchronously rather than rejected: this can only happen
			// if something other than the generated client called us.
			panic(js.Error{Value: errorf("%s: expected a single JSON string argument", e.name)})
		}
		raw := args[0].String()

		executor := js.FuncOf(func(_ js.Value, p []js.Value) any {
			resolve, reject := p[0], p[1]
			go func() {
				out, err := e.invoke(raw)
				if err != nil {
					reject.Invoke(toJSError(err))
					return
				}
				resolve.Invoke(out)
			}()
			return js.Undefined()
		})
		promise := promiseCtor.New(executor)
		// Safe: per the Promise spec the executor has already run, synchronously,
		// by the time the constructor returns. Without this every call leaks a
		// slot in the js.Value table.
		executor.Release()
		return promise
	})
}

// Run installs every registered function on a fresh namespace object, signals
// readiness to the loader, and blocks until dispose() is called from JS.
//
// The namespace id comes from argv when the loader supplies one, so two
// instances in the same process or page cannot clobber each other —
// js.Global().Set writes the real global object.
func Run(namespace string) {
	ns := js.Global().Get("Object").New()
	funcs := make([]js.Func, 0, len(registry)+1)

	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		f := wrap(registry[name])
		funcs = append(funcs, f)
		ns.Set(name, f)
	}

	done := make(chan struct{})
	var closed bool
	dispose := js.FuncOf(func(js.Value, []js.Value) any {
		if !closed {
			closed = true
			close(done)
		}
		return js.Undefined()
	})
	funcs = append(funcs, dispose)
	ns.Set("dispose", dispose)

	js.Global().Set(namespace, ns)

	// go.run()'s promise resolves when main() *exits*, so it is useless as a
	// readiness signal. The loader installs this callback and awaits it instead.
	if ready := js.Global().Get("__gowasmReady"); ready.Type() == js.TypeFunction {
		ready.Invoke(namespace)
	}

	// Block main() so the runtime and every registered js.Func stay alive. A
	// bare `select {}` would do that too but offers no way back out; receiving
	// on a channel dispose() can close gives us a real shutdown path.
	<-done

	for _, f := range funcs {
		f.Release()
	}
	js.Global().Delete(namespace)
}

// Namespace resolves the namespace id the loader asked for, falling back to the
// name baked in at generation time.
func Namespace(args []string, fallback string) string {
	if len(args) > 1 && args[1] != "" {
		return args[1]
	}
	return fallback
}

// ParseJSON is a helper for generated code that needs to hand a js.Value back
// rather than a Go value. Unused by the default JSON path; kept because the
// generated bridge references it when a function already returns raw JSON.
func ParseJSON(s string) js.Value { return jsonObj.Call("parse", s) }

// normalizeNils replaces nil slices and maps with empty ones, recursively,
// before a result is marshalled.
//
// Without this the generated types would be unsound: a Go function whose result
// slice is nil marshals to JSON null, while the TypeScript signature promises
// T[]. Callers would hit "cannot read properties of null" on a value the type
// checker told them was an array. Normalising here keeps the wire matching the
// declared types, and an empty collection is what a nil one means anyway.
func normalizeNils(v reflect.Value) {
	switch v.Kind() {
	case reflect.Slice:
		if v.IsNil() {
			if v.CanSet() {
				v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			}
			return
		}
		// []byte is marshalled as a base64 string, so its elements are not
		// worth walking.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		for i := range v.Len() {
			normalizeNils(v.Index(i))
		}

	case reflect.Array:
		for i := range v.Len() {
			normalizeNils(v.Index(i))
		}

	case reflect.Map:
		if v.IsNil() {
			if v.CanSet() {
				v.Set(reflect.MakeMap(v.Type()))
			}
			return
		}
		// Map values are not addressable, so each one is normalised in a copy
		// and written back.
		for _, key := range v.MapKeys() {
			entry := reflect.New(v.Type().Elem()).Elem()
			entry.Set(v.MapIndex(key))
			normalizeNils(entry)
			v.SetMapIndex(key, entry)
		}

	case reflect.Pointer:
		if !v.IsNil() {
			normalizeNils(v.Elem())
		}

	case reflect.Interface:
		if v.IsNil() {
			return
		}
		inner := reflect.New(v.Elem().Type()).Elem()
		inner.Set(v.Elem())
		normalizeNils(inner)
		if v.CanSet() {
			v.Set(inner)
		}

	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				normalizeNils(f)
			}
		}
	}
}

// normalized returns result with its nil collections filled in.
func normalized(result any) any {
	if result == nil {
		return nil
	}
	// An addressable copy, because reflect cannot set through an interface.
	box := reflect.New(reflect.TypeOf(result)).Elem()
	box.Set(reflect.ValueOf(result))
	normalizeNils(box)
	return box.Interface()
}
