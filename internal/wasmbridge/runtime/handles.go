//go:build js && wasm

package runtime

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// A handle is an integer standing in for a Go value that outlives the call
// which produced it.
//
// Most values cross the boundary as JSON and are done with. A value with
// methods cannot: calling one has to reach the same object the last call
// mutated, and JavaScript has no way to hold a Go pointer. So the object stays
// here and JavaScript holds a number.
//
// The table is per-instance in the only sense that matters: each wasm instance
// is its own Go runtime with its own linear memory, so this package-level state
// is not shared between them. That is also why a handle from one instance is
// meaningless in another, and why the generated classes check.

type handleEntry struct {
	val any
	// inUse counts calls currently executing against this handle. Release
	// cannot simply delete: every call body runs on its own goroutine, so a
	// method issued before close() can still be reading when close() arrives.
	// Deleting under it would fail a call the caller had every right to make.
	inUse int
	// released marks a tombstone: no new call may borrow it, and the entry is
	// dropped once the last in-flight borrower is done.
	released bool
}

var (
	handleMu sync.Mutex
	handles  = map[int64]*handleEntry{}
	// nextHandle starts at 1 so 0 is never issued. That lets a nil pointer
	// round-trip as 0 -> null without a second form on the wire.
	nextHandle int64 = 1
)

// Retain stores a value and returns its handle.
//
// The caller is responsible for not passing a typed nil: generated code checks
// for that where the static type is known, because a nil *T inside an `any` is
// not itself nil and no check here could see it.
func Retain(v any) int64 {
	handleMu.Lock()
	defer handleMu.Unlock()
	h := nextHandle
	nextHandle++
	handles[h] = &handleEntry{val: v}
	return h
}

// Borrow resolves a handle for the duration of one call, and returns the
// function that ends the borrow. The caller must always call it, normally with
// defer.
func Borrow(h int64) (any, func(), error) {
	handleMu.Lock()
	defer handleMu.Unlock()

	e, live := handles[h]
	if !live || e.released {
		return nil, nil, fmt.Errorf("handle %d is not live; it was closed, or it belongs to another instance", h)
	}
	e.inUse++

	var once sync.Once
	done := func() {
		once.Do(func() {
			handleMu.Lock()
			defer handleMu.Unlock()
			e.inUse--
			if e.released && e.inUse == 0 {
				delete(handles, h)
			}
		})
	}
	return e.val, done, nil
}

// Release marks a handle unusable and drops it once no call is still running
// against it.
//
// Releasing an unknown handle is not an error: close() is idempotent by
// contract, and a handle whose instance is gone is already released in every
// sense the caller cares about.
func Release(h int64) error {
	handleMu.Lock()
	defer handleMu.Unlock()

	e, live := handles[h]
	if !live {
		return nil
	}
	e.released = true
	if e.inUse == 0 {
		delete(handles, h)
	}
	return nil
}

// LiveHandles reports how many handles are still held.
//
// This exists to be observable. The shared instance behind the generated named
// exports is never disposed, so a caller who forgets close() grows this table
// for the life of the process with nothing to see. A number that can be read
// from a test, or from a console, is the difference between a diagnosable leak
// and a mysterious one.
func LiveHandles() int {
	handleMu.Lock()
	defer handleMu.Unlock()
	return len(handles)
}

// releaseAll drops every handle, closing anything that knows how, and reports
// how many were still live.
//
// Called from Run once the instance is being torn down. Without it, "dispose
// closes everything" would be false for exactly the values most likely to hold
// an operating-system resource.
func releaseAll() int {
	handleMu.Lock()
	live := make([]*handleEntry, 0, len(handles))
	for h, e := range handles {
		live = append(live, e)
		delete(handles, h)
	}
	handleMu.Unlock()

	for _, e := range live {
		if c, ok := e.val.(io.Closer); ok {
			// A failing Close during teardown has nowhere useful to go: the
			// instance is going away regardless, and there is no caller left to
			// tell.
			_ = c.Close()
		}
	}
	return len(live)
}

// The two functions the runtime installs for handle bookkeeping. A Go
// identifier cannot begin with two underscores and generated wire names are
// built from Go identifiers, so neither can ever collide with a user's export.
func init() {
	Register("__release", 1, func(args []json.RawMessage) (any, error) {
		var h int64
		if err := json.Unmarshal(args[0], &h); err != nil {
			return nil, fmt.Errorf("__release: %w", err)
		}
		return nil, Release(h)
	})

	Register("__stats", 0, func([]json.RawMessage) (any, error) {
		return map[string]int{"live": LiveHandles()}, nil
	})
}
