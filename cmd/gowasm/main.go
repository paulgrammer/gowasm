// Command gowasm turns a Go package into a typed npm package built on
// WebAssembly.
package main

import (
	"os"

	"github.com/paulgrammer/gowasm/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:])) }
