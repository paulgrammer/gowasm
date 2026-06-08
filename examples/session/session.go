// Package session is a key/value store with identity.
//
// It exists to show what gowasm does with a Go type that has methods. Store is
// never copied across the boundary: JavaScript holds a handle, every member is
// a call, and a method always sees the object as the last one left it.
//
//	await using s = (await open("cart", 8))!;
//	await s.set("sku", "1234");
//	await s.get("sku");
//
// Snapshot, by contrast, has no methods and is only ever used as a value, so it
// stays a plain interface and crosses as JSON. Both kinds of type live here on
// purpose.
package session

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Snapshot is a point-in-time copy of a store. It is data: no methods, used
// only by value, so it crosses the boundary as an ordinary object.
type Snapshot struct {
	Name  string   `json:"name"`
	Keys  []string `json:"keys"`
	Count int      `json:"count"`
}

// Store holds key/value pairs. Its exported fields are readable and writable
// from JavaScript, but always by asking Go, never from a copy.
type Store struct {
	// Name labels the store. It is an exported field, so it becomes a getter
	// and a setter on the generated class.
	Name string `json:"name"`
	// Limit caps how many keys the store will hold.
	Limit int `json:"limit"`
	// Writes counts every successful Set, which is what makes staleness
	// visible: read it, write, read it again.
	Writes int `json:"writes"`

	items  map[string]string
	parent *Store
	closed bool
}

// Txn batches writes until it is committed. It is a second class, and Store
// and Txn refer to each other, which is why the generated classes share one
// module rather than importing across files.
type Txn struct {
	store   *Store
	pending map[string]string
}

// New returns an empty store with a default limit.
func New(name string) *Store {
	return &Store{Name: name, Limit: 8, items: map[string]string{}}
}

// Open returns a store, or an error when the limit makes no sense. A
// constructor that can fail becomes a rejected promise like any other call.
func Open(name string, limit int) (*Store, error) {
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("a store needs a name")
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive, got %d", limit)
	}
	return &Store{Name: name, Limit: limit, items: map[string]string{}}, nil
}

// Ping is a plain function, to show free functions and classes coexisting.
func Ping() string { return "session" }

// Set stores a value.
func (s *Store) Set(key, value string) error {
	if s.closed {
		return fmt.Errorf("%s is closed", s.Name)
	}
	if key == "" {
		return fmt.Errorf("a key is required")
	}
	if _, replacing := s.items[key]; !replacing && len(s.items) >= s.Limit {
		return fmt.Errorf("%s is full at %d keys", s.Name, s.Limit)
	}
	s.items[key] = value
	s.Writes++
	return nil
}

// Get reads a value, or reports that it is missing.
func (s *Store) Get(key string) (string, error) {
	v, ok := s.items[key]
	if !ok {
		return "", fmt.Errorf("no key %q in %s", key, s.Name)
	}
	return v, nil
}

// Keys lists what the store holds, in order. An empty store returns an empty
// array rather than null.
func (s *Store) Keys() []string {
	out := make([]string, 0, len(s.items))
	for k := range s.items {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Snapshot copies the store into a plain value, which crosses as JSON.
func (s *Store) Snapshot() Snapshot {
	return Snapshot{Name: s.Name, Keys: s.Keys(), Count: len(s.items)}
}

// Blob returns a value as bytes, so a method result goes through the same
// binary conversion a function result would.
func (s *Store) Blob(key string) ([]byte, error) {
	v, err := s.Get(key)
	if err != nil {
		return nil, err
	}
	return []byte(v), nil
}

// Tags returns the keys that are present, out of the ones asked for. Variadic
// on a method behaves exactly as it does on a function.
func (s *Store) Tags(keys ...string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if _, ok := s.items[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// Count reports how many keys are held. The context is dropped from the
// JavaScript signature, the same as on a free function.
func (s *Store) Count(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return len(s.items), nil
}

// Merge copies another store's keys into this one. A class as a parameter
// travels as its handle.
func (s *Store) Merge(other *Store) error {
	if other == nil {
		return fmt.Errorf("nothing to merge")
	}
	for _, k := range other.Keys() {
		if err := s.Set(k, other.items[k]); err != nil {
			return err
		}
	}
	return nil
}

// Child returns a new store owned by this one, so a method can return another
// handle.
func (s *Store) Child(name string) *Store {
	c := New(name)
	c.parent = s
	return c
}

// Parent returns the store this one came from, or nothing. A nil pointer
// arrives as null.
func (s *Store) Parent() *Store { return s.parent }

// Txn starts a batch of writes.
func (s *Store) Txn() *Txn {
	return &Txn{store: s, pending: map[string]string{}}
}

// Label describes the store. A value receiver is exposed like any other
// method; it simply cannot mutate anything.
func (s Store) Label() string {
	return fmt.Sprintf("%s (%d/%d)", s.Name, len(s.items), s.Limit)
}

// Close marks the store unusable. gowasm folds it into the generated close(),
// so releasing the handle also closes the store.
func (s *Store) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return nil
}

// String makes Store a fmt.Stringer. It is a conventional method, so it does
// not on its own decide anything about how the type crosses the boundary.
func (s *Store) String() string { return s.Label() }

// Set stages a write.
func (t *Txn) Set(key, value string) error {
	if key == "" {
		return fmt.Errorf("a key is required")
	}
	t.pending[key] = value
	return nil
}

// Commit applies every staged write.
func (t *Txn) Commit() (int, error) {
	keys := make([]string, 0, len(t.pending))
	for k := range t.pending {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := t.store.Set(k, t.pending[k]); err != nil {
			return 0, err
		}
	}
	n := len(keys)
	t.pending = map[string]string{}
	return n, nil
}

// Store returns the store this transaction writes to, which is how two classes
// come to refer to each other.
func (t *Txn) Store() *Store { return t.store }
