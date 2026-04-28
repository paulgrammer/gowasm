# gowasm

Turn a Go package into a typed npm package built on WebAssembly.

```sh
npm install -g @paulgrammer/gowasm
gowasm doctor
```

Or without installing:

```sh
npx @paulgrammer/gowasm init
```

Whichever way you install it, the command it puts on your PATH is `gowasm`.
The package is scoped because npm refuses the bare name: it strips punctuation
when comparing, so `gowasm` collides with `go-wasm`, an abandoned package from
2018.

This package is a launcher. The binary itself comes from a small
platform-specific package installed as an optional dependency, so npm downloads
only the one binary your machine can run, and no install script has to fetch
anything.

gowasm drives the Go and Node toolchains rather than bundling them, so it needs
Go 1.24+ and Node 20+ on your PATH. Run `gowasm doctor` to check.

Full documentation: https://github.com/paulgrammer/gowasm
