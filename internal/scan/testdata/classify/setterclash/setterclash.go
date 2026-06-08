// Package setterclash has a field whose generated setter collides with a
// hand-written method.
package setterclash

type Store struct {
	Name string `json:"name"`
}

func New() *Store                 { return &Store{} }
func (s *Store) SetName(v string) { s.Name = v }
func (s *Store) Ping() string     { return s.Name }
