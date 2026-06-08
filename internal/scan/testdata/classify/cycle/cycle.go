// Package cycle is the case that cannot be decided while walking lazily: the
// value use of Store is reachable only through Store's own fields.
//
// If Store were assumed to be a handle, its fields would never be walked, Meta
// would never be discovered, and Origin would never be seen. The answer would
// depend on the answer.
package cycle

type Store struct {
	Parent *Store
	Tags   []Meta
}

type Meta struct {
	Origin Store `json:"origin"`
}

func New() *Store             { return &Store{} }
func (s *Store) Touch() error { return nil }
