// Package handle is the clean case: methods, and only ever behind a pointer.
package handle

type Store struct {
	Name  string `json:"name"`
	Limit int    `json:"limit"`
	items map[string]string
}

// Snapshot has no methods and is used by value, so it stays plain data.
type Snapshot struct {
	Keys []string `json:"keys"`
}

func New(name string) *Store                  { return &Store{Name: name} }
func (s *Store) Get(k string) (string, error) { return s.items[k], nil }
func (s *Store) Snapshot() Snapshot           { return Snapshot{} }
func (s Store) Label() string                 { return s.Name }
