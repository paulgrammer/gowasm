// Package blob shows binary data crossing the WebAssembly boundary.
//
// Go []byte appears in TypeScript as Uint8Array. encoding/json actually moves
// the data base64-encoded, but the generated client converts at the boundary,
// so callers work with typed arrays and never see the encoding — including for
// binary nested inside structs and slices.
package blob

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
)

// Checksum is a digest with the algorithm that produced it. Its Sum field is
// binary nested inside a struct, so the generated client converts it in place.
type Checksum struct {
	Algo string `json:"algo"`
	Sum  []byte `json:"sum"`
	// Size is the number of bytes hashed.
	Size int `json:"size"`
}

// Digest hashes data with SHA-256.
func Digest(data []byte) Checksum {
	sum := sha256.Sum256(data)
	return Checksum{Algo: "sha256", Sum: sum[:], Size: len(data)}
}

// Compress gzips data.
func Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	// Fixed compression level so the output is byte-for-byte reproducible,
	// which lets the recorded test fixtures stay stable.
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(data); err != nil {
		return nil, fmt.Errorf("compressing: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("finishing gzip stream: %w", err)
	}
	return buf.Bytes(), nil
}

// Decompress reverses Compress, rejecting input that is not a gzip stream.
func Decompress(data []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("reading gzip header: %w", err)
	}
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decompressing: %w", err)
	}
	return out, nil
}

// Concat joins any number of chunks, demonstrating a variadic binary parameter.
// In TypeScript this is concat(...chunks: Uint8Array[]).
func Concat(chunks ...[]byte) []byte {
	return bytes.Join(chunks, nil)
}

// Split cuts data into fixed-size chunks, demonstrating a [][]byte result,
// which reaches TypeScript as Uint8Array[].
func Split(data []byte, size int) ([][]byte, error) {
	if size <= 0 {
		return nil, fmt.Errorf("chunk size must be positive, got %d", size)
	}
	var out [][]byte
	for start := 0; start < len(data); start += size {
		end := min(start+size, len(data))
		out = append(out, data[start:end])
	}
	return out, nil
}
