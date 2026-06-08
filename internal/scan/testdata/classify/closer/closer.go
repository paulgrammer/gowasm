// Package closer declares Close() error, which is folded into the generated
// close() rather than exposed beside it.
package closer

type File struct {
	Path string `json:"path"`
}

func Open(p string) *File    { return &File{Path: p} }
func (f *File) Read() string { return "" }
func (f *File) Close() error { return nil }
