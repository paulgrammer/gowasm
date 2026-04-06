package blob_test

import (
	"fmt"

	"example.com/blob"
)

func ExampleDigest() {
	sum := blob.Digest([]byte("hello"))
	fmt.Println(sum.Algo, sum.Size)
	// Output: sha256 5
}

func ExampleCompress() {
	packed, _ := blob.Compress([]byte("hello hello hello hello"))
	fmt.Println(len(packed) > 0)
	// Output: true
}

func ExampleDecompress_invalid() {
	_, err := blob.Decompress([]byte("not gzip"))
	fmt.Println(err != nil)
	// Output: true
}

func ExampleConcat() {
	fmt.Println(string(blob.Concat([]byte("go"), []byte("wasm"))))
	// Output: gowasm
}

func ExampleSplit() {
	chunks, _ := blob.Split([]byte("abcdefg"), 3)
	fmt.Println(len(chunks))
	// Output: 3
}

func ExampleSplit_invalidSize() {
	_, err := blob.Split([]byte("abc"), 0)
	fmt.Println(err)
	// Output: chunk size must be positive, got 0
}
