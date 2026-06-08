// Package sliceof returns a slice of pointers. A handle can only be a whole
// parameter or result, so this is a value use and the type cannot be a class.
package sliceof

type Store struct {
	Name string `json:"name"`
}

func All() []*Store           { return nil }
func One() *Store             { return nil }
func (s *Store) Ping() string { return s.Name }
