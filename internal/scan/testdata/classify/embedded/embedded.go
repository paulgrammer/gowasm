// Package embedded promotes methods from an embedded type. A promoted Lock
// must not become callable: a lock() from JS with no unlock() would wedge the
// instance permanently.
package embedded

import "sync"

type Store struct {
	sync.Mutex
	Name string `json:"name"`
}

func New() *Store             { return &Store{} }
func (s *Store) Ping() string { return s.Name }
