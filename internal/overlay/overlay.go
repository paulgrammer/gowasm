// Package overlay presents generated Go source to the go command without
// writing it into the user's repository.
//
// The bridge that registers each exported function has to live in a package
// inside the user's module, so it can import both their code and the runtime.
// Writing it to disk would leave a generated directory in every project that
// uses gowasm, which then has to be gitignored, explained, and kept from
// drifting. `go build -overlay` avoids all of that: the file exists only for
// the duration of the build, in a temporary directory, and the go command is
// told to pretend it sits at a path inside the module.
package overlay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Overlay is a prepared set of virtual files.
type Overlay struct {
	// Path is the overlay manifest to pass to `go build -overlay`.
	Path string

	tmpDir string
}

// spec is the manifest format the go command reads.
type spec struct {
	Replace map[string]string `json:"Replace"`
}

// New stages files at the given virtual paths, each of which must be an
// absolute path inside the module being built. Nothing is written there; the
// files live in a temporary directory and the manifest points the go command at
// them.
//
// Several files may share a virtual directory, which is how the generated
// registrations and the runtime end up in one package without either being
// written to disk.
func New(files map[string][]byte) (*Overlay, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("overlay needs at least one file")
	}

	tmp, err := os.MkdirTemp("", "gowasm-overlay-")
	if err != nil {
		return nil, err
	}
	o := &Overlay{tmpDir: tmp}

	replace := make(map[string]string, len(files))
	for virtualPath, content := range files {
		if !filepath.IsAbs(virtualPath) {
			o.Close()
			return nil, fmt.Errorf("overlay target must be absolute, got %q", virtualPath)
		}
		real := filepath.Join(tmp, filepath.Base(virtualPath))
		if err := os.WriteFile(real, content, 0o644); err != nil {
			o.Close()
			return nil, err
		}
		replace[virtualPath] = real
	}

	manifest, err := json.Marshal(spec{Replace: replace})
	if err != nil {
		o.Close()
		return nil, err
	}

	o.Path = filepath.Join(tmp, "overlay.json")
	if err := os.WriteFile(o.Path, manifest, 0o644); err != nil {
		o.Close()
		return nil, err
	}
	return o, nil
}

// Close removes the staged files.
func (o *Overlay) Close() error {
	if o.tmpDir == "" {
		return nil
	}
	return os.RemoveAll(o.tmpDir)
}
