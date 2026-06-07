// Package store is a small immutable key/value map.
//
// Every operation returns a new Store rather than mutating one, so a call is
// worth exactly its result: the same call always produces the same value,
// whatever ran before it.
package store

import (
	"fmt"
	"sort"
)

// Options configures a new store. It is the third Options in this project, and
// none of the three had to be renamed.
type Options struct {
	Namespace string `json:"namespace"`
	// Limit caps how many keys the store will hold.
	Limit int `json:"limit"`
}

// Store is a namespaced set of key/value pairs.
type Store struct {
	Namespace string            `json:"namespace"`
	Limit     int               `json:"limit"`
	Items     map[string]string `json:"items"`
}

// New returns the default options for a namespace.
func New(namespace string) Options {
	return Options{Namespace: namespace, Limit: 16}
}

// Open creates an empty store.
func Open(opts Options) Store {
	if opts.Limit <= 0 {
		opts.Limit = 16
	}
	return Store{Namespace: opts.Namespace, Limit: opts.Limit, Items: map[string]string{}}
}

// Put returns a copy of s with key set.
func Put(s Store, key, value string) (Store, error) {
	if key == "" {
		return Store{}, fmt.Errorf("a key is required")
	}
	items := make(map[string]string, len(s.Items)+1)
	for k, v := range s.Items {
		items[k] = v
	}
	if _, replacing := items[key]; !replacing && len(items) >= s.Limit {
		return Store{}, fmt.Errorf("%s is full at %d keys", s.Namespace, s.Limit)
	}
	items[key] = value
	s.Items = items
	return s, nil
}

// Get reads a key, or reports that it is missing.
func Get(s Store, key string) (string, error) {
	v, ok := s.Items[key]
	if !ok {
		return "", fmt.Errorf("no key %q in %s", key, s.Namespace)
	}
	return v, nil
}

// Keys lists what the store holds, in order.
func Keys(s Store) []string {
	out := make([]string, 0, len(s.Items))
	for k := range s.Items {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
