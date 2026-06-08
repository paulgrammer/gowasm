//go:build js && wasm

package runtime

import (
	"sync"
	"testing"
)

func reset() {
	handleMu.Lock()
	defer handleMu.Unlock()
	handles = map[int64]*handleEntry{}
	nextHandle = 1
}

// 0 is the wire form of a nil pointer, so it must never name a real object.
func TestHandleZeroIsNeverIssued(t *testing.T) {
	reset()
	for i := 0; i < 100; i++ {
		if h := Retain(i); h == 0 {
			t.Fatal("Retain issued handle 0, which is reserved for nil")
		}
	}
}

func TestBorrowResolvesAndReleaseInvalidates(t *testing.T) {
	reset()
	h := Retain("value")

	v, done, err := Borrow(h)
	if err != nil || v != "value" {
		t.Fatalf("Borrow(%d) = %v, %v; want the stored value", h, v, err)
	}
	done()

	if err := Release(h); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, _, err := Borrow(h); err == nil {
		t.Error("borrowing a released handle should fail")
	}
	if n := LiveHandles(); n != 0 {
		t.Errorf("LiveHandles = %d after release, want 0", n)
	}
}

// close() is idempotent by contract, and a handle whose instance is gone is
// already released in every sense the caller cares about.
func TestReleaseIsIdempotent(t *testing.T) {
	reset()
	h := Retain("value")
	for i := 0; i < 3; i++ {
		if err := Release(h); err != nil {
			t.Fatalf("Release #%d: %v", i+1, err)
		}
	}
	if err := Release(9999); err != nil {
		t.Errorf("releasing an unknown handle should be a no-op, got %v", err)
	}
}

// The race this table exists to prevent: a call issued before close() must
// still complete. Deleting the entry out from under it would fail a call the
// caller had every right to make.
func TestReleaseDuringBorrowDefersTheDelete(t *testing.T) {
	reset()
	h := Retain("value")

	v, done, err := Borrow(h)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}

	// close() arrives while the call is still running.
	if err := Release(h); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if n := LiveHandles(); n != 1 {
		t.Errorf("LiveHandles = %d while a borrow is outstanding, want 1", n)
	}
	if v != "value" {
		t.Errorf("the borrowed value changed to %v", v)
	}
	// A release is a tombstone: no *new* call may reach it.
	if _, _, err := Borrow(h); err == nil {
		t.Error("a second borrow after release should fail")
	}

	done()
	if n := LiveHandles(); n != 0 {
		t.Errorf("LiveHandles = %d after the last borrower finished, want 0", n)
	}
}

func TestDoneIsIdempotent(t *testing.T) {
	reset()
	h := Retain("value")
	_, done, err := Borrow(h)
	if err != nil {
		t.Fatalf("Borrow: %v", err)
	}
	done()
	done() // a generated defer plus an explicit call must not double-decrement

	other := Retain("second")
	if _, _, err := Borrow(other); err != nil {
		t.Errorf("an unrelated handle was disturbed: %v", err)
	}
}

// Every call body runs on its own goroutine, so the table is shared mutable
// state. Run with -race.
func TestConcurrentRetainYieldsDistinctHandles(t *testing.T) {
	reset()
	const n = 200

	var wg sync.WaitGroup
	got := make([]int64, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i] = Retain(i)
		}(i)
	}
	wg.Wait()

	seen := map[int64]bool{}
	for _, h := range got {
		if seen[h] {
			t.Fatalf("handle %d was issued twice", h)
		}
		seen[h] = true
	}
	if LiveHandles() != n {
		t.Errorf("LiveHandles = %d, want %d", LiveHandles(), n)
	}
}

type closable struct{ closed *bool }

func (c closable) Close() error {
	*c.closed = true
	return nil
}

// Teardown is the only place "dispose closes everything" can be made true.
func TestReleaseAllClosesWhatItCan(t *testing.T) {
	reset()
	var closed bool
	Retain(closable{closed: &closed})
	Retain("not a closer") // must not panic

	if n := releaseAll(); n != 2 {
		t.Errorf("releaseAll swept %d handles, want 2", n)
	}
	if !closed {
		t.Error("a value implementing io.Closer was dropped without being closed")
	}
	if n := LiveHandles(); n != 0 {
		t.Errorf("LiveHandles = %d after teardown, want 0", n)
	}
}
