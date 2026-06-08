# gowasm

Turn a Go package into a typed npm package, built on WebAssembly.

You write ordinary Go. `gowasm` compiles it to WebAssembly and generates the
whole TypeScript side around it — types, a typed client, the loader, and tests.
There is no TypeScript for you to write and no annotations to add to your Go.

```go
// urls/urls.go
type Strictness string

const (
	Relaxed Strictness = "relaxed"
	Strict  Strictness = "strict"
)

type Match struct {
	Raw    string `json:"raw"`
	Scheme string `json:"scheme,omitempty"`
	Host   string `json:"host"`
}

func ExtractURLs(text string, mode Strictness) ([]Match, error) { … }
```

```console
$ gowasm init
$ gowasm build
```

```ts
import { extractURLs } from "@acme/urls";
import type { Match } from "@acme/urls";

const matches: Match[] = await extractURLs("see https://go.dev", "relaxed");
//                                                               ^ autocompletes
//                                                                 "relaxed" | "strict"
matches[0].host; // "go.dev"
```

The published package needs neither Go nor gowasm to use: the compiled module
ships inside it, and it runs on Node 20+ and in browsers through any bundler.

## Install

### npm

```sh
npm install -g @paulgrammer/gowasm
```

Or run it without installing:

```sh
npx @paulgrammer/gowasm init
npx @paulgrammer/gowasm build
```

Or pin it to a project, which is usually what you want so everyone building the
repository uses the same version:

```sh
npm install --save-dev @paulgrammer/gowasm
```

```jsonc
// package.json
{
  "scripts": {
    "build:wasm": "gowasm build",
    "test:wasm": "gowasm test"
  },
  "devDependencies": {
    "@paulgrammer/gowasm": "^0.5.0"
  }
}
```

The npm package is a launcher; the binary comes from a small platform-specific
package (`gowasm-linux-amd64` and friends) installed as an optional dependency.
npm resolves the `os` and `cpu` fields and fetches only the binary your machine
can run, and no install script downloads anything — so it works under
`--ignore-scripts`, from an offline cache, and in locked-down CI.

However it is installed, the command it puts on your PATH is `gowasm`.

The package is scoped because npm refuses the bare name: it strips punctuation
when comparing, so `gowasm` collides with `go-wasm`, an abandoned package from
2018. Yarn, pnpm and bun honour the same `os` and `cpu` fields, so
`yarn add -D @paulgrammer/gowasm` and the equivalents work too.

### Shell script

```sh
curl -fsSL https://raw.githubusercontent.com/paulgrammer/gowasm/main/install.sh | sh
```

### Go

```sh
go install github.com/paulgrammer/gowasm/cmd/gowasm@latest
```

### After installing

Check the toolchain before anything else:

```sh
gowasm doctor
```

Requires Go 1.24+ (for `lib/wasm`) and Node 20+. gowasm drives both rather than
bundling them, and `gowasm doctor` says so if either is missing.

The install script verifies the release checksum before installing, and refuses
to install anything that does not match. It takes two environment variables:

| Variable | Default |
| --- | --- |
| `GOWASM_VERSION` | the latest release tag |
| `GOWASM_INSTALL` | `/usr/local/bin`, falling back to `~/.local/bin` |

Installing through `curl` and `tar` also sidesteps the `com.apple.quarantine`
attribute a browser download would set, so macOS Gatekeeper does not flag the
binary even when a release is unsigned.

While the repository is private its release assets are not publicly
downloadable; the script detects that and falls back to `gh release download`,
using your existing GitHub CLI login.

## Commands

| Command | Does |
| --- | --- |
| `gowasm init` | Interactive setup, in the style of `npm init`. Writes `gowasm.yaml`. |
| `gowasm generate` | Scan the Go package and write the TypeScript package. No compilation. |
| `gowasm build` | Generate, compile to WebAssembly, build the npm package. |
| `gowasm test` | Build, then run the generated tests on Node. |
| `gowasm publish` | Build, then hand the package to `npm publish`. |
| `gowasm doctor` | Check that the toolchain can build and run WebAssembly. |

Add `-y` to accept every default, `-C dir` to run elsewhere, `-v` to echo the
commands being run, and `-bridge file` to inspect the generated Go glue.

## What gets generated

```
node/
  package.json  tsconfig.json  README.md  LICENSE
  src/index.node.ts            entry points, one per target
  src/generated/types.ts       interfaces, enum unions, aliases
  src/generated/client.ts      the typed API
  src/generated/codec.ts       binary conversion, when the package uses []byte
  src/runtime/                 loader, readiness handshake, error mapping
  src/vendor/wasm_exec.js      copied from your GOROOT
  src/main.wasm
  test/contract.gen.test.ts    derived from your signatures
  test/fixtures.gen.test.ts    recorded from your Go Example functions
```

Files ending in `.gen.test.ts` are rewritten on every run. A test file without
that infix is yours and is never touched.

A project exposing several Go packages gets one `src/generated/<namespace>/`
directory and one `src/<namespace>.<target>.ts` entry per package, and its test
files are prefixed the same way — see [Several packages](#several-packages).

Nothing is written into your own source tree — not even the Go glue that
registers your functions, nor the runtime that serves it. Both are supplied to
the compiler through `go build -overlay`, so there is no generated directory to
gitignore or to drift out of date.

Your `go.mod` gains nothing either: gowasm is a build-time tool, not a
dependency. The runtime is emitted alongside the generated code, which also
means it can never fall out of step with the generator that produced its
caller — both come from the same binary.

## The rules it follows

Every exported function in the package crosses the boundary. There are no
marker comments: write a Go function, run `gowasm build`, call it from
TypeScript.

| Go | TypeScript |
| --- | --- |
| `string`, `bool` | `string`, `boolean` |
| `int`…`int32`, `uint`…`uint32`, `float32/64` | `number` |
| `int64`, `uint64` | `number`, with a precision note (or `string`, see `int64:`) |
| `[]byte` | `Uint8Array` |
| `[]T` | `T[]` |
| `map[K]V` | `Record<string, V>` |
| `*T` | `T \| null` |
| `time.Time` | `ISODateTime` |
| named struct, used by value | `interface`, embedded structs become `extends` |
| named struct with methods, only ever `*T` | a `class` with identity — see [Types with methods](#types-with-methods) |
| named scalar with constants | a literal union — a real enum |
| named scalar without constants | a type alias, keeping the name |
| `interface{}` | `unknown` |
| `chan`, `func`, `complex`, type parameters | **refused**, with a `file:line` error |

That last row is the point: a channel has no honest TypeScript equivalent, so
generation stops rather than emitting `unknown` and failing at runtime instead.

Signatures follow the same idea. A leading `context.Context` is dropped from the
JavaScript signature; a trailing `error` becomes a rejected promise; several
non-error results become a tuple; a variadic parameter becomes a rest parameter.

## Types with methods

A struct used as a value is data: it crosses as JSON and has no identity. A
struct with exported methods that is only ever seen behind a pointer is a
*resource*, and becomes a class:

```go
type Store struct {
	Name string `json:"name"`
}

func Open(name string) (*Store, error)
func (s *Store) Set(key, value string) error
func (s *Store) Get(key string) (string, error)
func (s *Store) Close() error
```

```ts
await using s = (await open("cart"))!;

await s.set("sku", "1234");
await s.get("sku");     // "1234"
await s.name;           // "cart" — read from Go, never from a copy
await s.setName("basket");
```

The object stays in Go and JavaScript holds a handle, so a method always sees
what the last one left. Exported fields are getters rather than properties for
the same reason: a value copied when the handle was made would be stale the
moment a method touched it, with nothing in the type to say so. Writes are
`setName(v)` because an assignment cannot be awaited, and a fire-and-forget
write would drop its error on the floor.

`close()` releases the handle, and calls a Go `Close() error` first if the type
has one. Bind it with `await using` and that happens at the end of the scope.
A `FinalizationRegistry` sweeps up handles nobody closed, but the specification
allows an engine never to run it, so it is a net rather than a guarantee —
`client.liveHandles()` is there to make a leak visible.

**Both conditions are required, and that is deliberate.** `examples/urls` returns
`*Match` from one function and `[]Match` from another, and `Match` is plain
data; keying on pointerness alone would have turned it into an opaque handle.
Nor do conventional methods count — `String`, `Error`, `MarshalJSON`, `Equal`
and friends — because adding a debug `String()` to a `Point` should not
restructure a published API.

A type with methods that is used *both* ways cannot be either, and generation
stops naming both positions rather than guessing. Say which you meant with a
`//gowasm:data` or `//gowasm:resource` comment on the type.

Some things follow from a handle being a handle: two handles to the same Go
object are two different JavaScript objects, so `===` is not identity; a handle
from one instance is refused by another rather than silently misread; and
`close()` resolving means no *new* call can reach the object, not that Go has
already dropped it — calls already in flight are allowed to finish.

See [`examples/session`](examples/session) for the whole surface.

Struct fields use their `json` tag. **A field with no tag keeps its Go name**,
because that is what `encoding/json` actually emits — camelCasing it would make
the type disagree with the payload. `json:"-"` and unexported fields are
skipped; `omitempty` makes the field optional.

## Promises, and why everything is async

A call from JavaScript re-enters the Go scheduler *synchronously*, inside the
JavaScript call stack, so a Go function that blocks would freeze the event loop.
Every exported function therefore returns a promise and runs on its own
goroutine. A Go `error` becomes a rejection carrying a real `Error`:

```ts
import { GoError } from "@acme/urls";

try {
  await parseWhen("nonsense");
} catch (err) {
  if (err instanceof GoError) console.error(err.message);
}
```

`await`, `try`/`catch`, `.catch()` and `Promise.all` all behave normally, and
concurrent calls interleave rather than queueing.

## Binary data

`[]byte` is a `Uint8Array` in TypeScript. On the wire it is base64, because that
is what `encoding/json` does, but the generated client converts at the boundary
— including for binary nested inside structs, slices and maps. Input is
accepted as a `Uint8Array`, an `ArrayBuffer`, a Node `Buffer`, or any typed
array; anything else fails with a clear message rather than encoding to empty.

## Generated tests

Two suites, from two different sources.

**Contract tests** come from your signatures: the module boots, every function
is exported, a missing or wrong-typed argument rejects, errors arrive as
`GoError`, concurrent calls settle independently, `dispose()` works and calls
after it are refused. They also contain type-level guards that make `tsc` fail
if code generation ever regresses to `any`.

**Fixture tests** come from your Go `Example` functions. Rather than translating
Go to TypeScript, gowasm extracts the calls with literal arguments, runs them
natively to record what your code actually returns, and replays them from
TypeScript. The expectations are produced by your own code, so they cannot drift
from it — change the Go, re-run, and they follow. Calls it cannot reproduce are
reported by name, never dropped silently.

## Demos

Reading a test suite tells you the code is correct; it does not show you
anything. `make demo` builds every browser-capable example and serves a page
per example that you can actually use:

```sh
make demo        # builds, then serves on http://localhost:8080
```

The regex one is the demonstration worth seeing first: it races V8's
backtracking engine against Go's RE2 on the same input, in the same tab. At
24 characters V8 takes seconds and the page freezes, because a backtracking
match cannot be interrupted. Go returns in well under a millisecond.

The chess board is playable: drag a piece, or tap one square then another.
Nothing about the rules lives in the page. It only draws the position Go
reports and only offers the moves Go has already called legal, so an illegal
drag simply snaps back, and a pawn reaching the last rank asks which piece to
promote to because Go returned four moves for those two squares.

Everything runs locally. The PDF, spreadsheet and binary demos exist partly to
make the point that none of it is uploaded anywhere.

## Examples

Each is a self-contained module. Run `gowasm test` inside any of them.

| Example | Shows |
| --- | --- |
| [`urls`](examples/urls) | The basics, and the only one targeting the browser as well as Node |
| [`regex`](examples/regex) | RE2: linear-time matching that cannot be made to hang |
| [`money`](examples/money) | Exact arithmetic where JavaScript numbers cannot be |
| [`text`](examples/text) | Unicode operations `Intl` does not cover |
| [`sanitize`](examples/sanitize) | Allowlist HTML cleaning with no DOM and no jsdom |
| [`expr`](examples/expr) | Evaluating user-written rules without `eval` |
| [`gofmt`](examples/gofmt) | Go's own parser and formatter, in the browser |
| [`highlight`](examples/highlight) | 297 languages, no per-language grammar fetch |
| [`excel`](examples/excel) | Reading and writing real `.xlsx`, entirely in memory |
| [`cue`](examples/cue) | A config language with no JavaScript implementation at all |
| [`git`](examples/git) | A real git repository in memory: commits, branches, diffs |
| [`image`](examples/image) | Blur, edges, tone and histograms, without a canvas |
| [`qr`](examples/qr) | QR codes, where the interesting part is the redundancy |
| [`chess`](examples/chess) | A domain model: enums, nested structs, legality |
| [`chip8`](examples/chip8) | A framebuffer arriving as a `Uint8Array` |
| [`blob`](examples/blob) | Binary exchange: gzip, digests, `Uint8Array[]`, variadic binary |
| [`worker-pool`](examples/worker-pool) | [Brandur Leach's Go worker pool](https://brandur.org/go-worker-pool), driven from Node |
| [`ginapi`](examples/ginapi) | A [Gin](https://gin-gonic.com) application behind a real Node HTTP server |
| [`multi`](examples/multi) | Three packages in one npm package, all three exporting `New` |
| [`session`](examples/session) | Go types with methods, as classes with identity |

The first few are the point of the tool: each is something the JavaScript
ecosystem either does worse or cannot do at all. `regex` is the sharpest —
JavaScript's engine backtracks, so a user-supplied pattern can hang the event
loop. Measured on `(a+)+$` against forty `a`s and a `b`: V8 takes 53 seconds,
Go returns in 0.6 milliseconds.

The Gin example is worth reading if you want an HTTP server. WebAssembly cannot
listen on a port — Go's `net` package there is an in-process fake that nothing
can dial — so Node keeps the socket and hands each request to the Go handler.
Routing, binding, validation and middleware all still run in Go.

## Configuration

```yaml
packages:
  - ./urls               # the Go packages to expose
out:     ./node          # where the npm package is written
npm:
  name:        "@acme/urls"
  version:     0.1.0
  description: URL extraction, compiled from Go
  license:     MIT
  author:      Ada Lovelace <ada@example.com>
  repository:  github.com/acme/urls
targets: [node, browser]
packageManager: npm      # npm | pnpm | yarn | bun
int64:   number          # number | string
```

### Several packages

`packages:` is a list, so a project laid out as `pkg/lib`, `pkg/store` and
`internal/api` exposes all three without a hand-written facade:

```yaml
packages:
  - ./pkg/lib            # namespace: lib
  - ./pkg/store          # namespace: store
  - path: ./internal/api # give a namespace explicitly when two packages
    as: admin            # would otherwise share a directory name
```

Each package gets its own namespace in TypeScript, so two of them may both
export `New` without either being renamed:

```ts
import { lib, store } from "@acme/thing";

const m: lib.Match = await lib.extract("…");
await store.get("key");
```

A namespace is also a subpath export, for consumers who want one package and
none of the others:

```ts
import { extract } from "@acme/thing/lib";
```

One WebAssembly module serves every namespace, however you import them:
splitting per package would multiply a multi-megabyte module by the number of
packages, and the Go runtime inside it is the bulk of that weight either way.

A project with a single package keeps the flat API — `extract(…)`, not
`lib.extract(…)` — because there is nothing to disambiguate. Adding a second
package therefore restructures the published API, so `gowasm generate` says so
rather than letting the change be discovered on the next install.

`gowasm init` detects the package manager from a lockfile if the project has
one, so an existing choice is honoured rather than overridden, and asks
otherwise. It is recorded in the config rather than detected on each run, so a
project does not change behaviour because of what happens to be installed on a
given machine.

Three differences between the managers are handled for you, because each fails
quietly rather than loudly: `bun test` runs Bun's own test runner and ignores
the package's test script, so scripts always go through `run`; yarn moved
publishing to `yarn npm publish` in version 2, so the right command depends on
which yarn is installed; and pnpm refuses to publish from a directory with
uncommitted changes, which a generated directory always has.

`gowasm init` fills this in by asking, inferring what it can from your project:
the package name from your Go module path, the author from `git config`, the
repository from your `origin` remote. Re-running it uses your existing answers
as the defaults.

## Publishing

```sh
gowasm publish
gowasm publish -- --access public     # first publish of a scoped package
gowasm publish -- --dry-run           # see what would be sent
gowasm publish -- --tag next --otp 123456
```

`gowasm publish` is a proxy: everything after `--` reaches `npm publish`
untouched, and gowasm adds no flags of its own. Authentication is npm's — log in
with `npm login`, or set `NODE_AUTH_TOKEN` in CI, exactly as for any other
package. gowasm never sees a credential.

It rebuilds first, so what gets published matches the Go source rather than
whatever happened to be left in `dist/`. Pass `-no-build` to publish exactly
what is on disk.

Only `dist/` is published, and it contains the compiled module, so consumers
need no Go toolchain. `cd node && npm publish` still works if you would rather
not go through gowasm at all.

## Releasing

```sh
make dist                  # cross-compile, archive, checksum
make dist VERSION=v1.2.3   # or pin the version explicitly
make npm-packages          # build the npm distribution into dist/npm/
make npm-publish           # publish it
```

`VERSION` defaults to `git describe`, so a build from a tagged commit reports
that tag without anything being edited. The version, commit and build date are
stamped in through `-ldflags` and shown by `gowasm -version`.

Binaries are cross-compiled with `CGO_ENABLED=0` for six platforms — darwin,
linux and windows on both `arm64` and `amd64` — so they are fully static. gowasm
has no cgo dependencies of its own; it shells out to the `go` and `npm` commands
already on your machine.

`make npm-packages` builds the npm distribution into `dist/npm/` and
`make npm-publish` pushes it, platform packages first so the launcher's
dependencies exist before the launcher does. `make unpack` restores `dist/`
from the release archives, so the npm packages can be built from the exact
binaries that were released rather than from a rebuild — the ldflags stamp a
build date, so a rebuild would not produce the same bytes.

Publishing to npm is its own workflow, triggered by the GitHub release being
published. Keeping it separate means a registry failure cannot leave the
release half-made, and it can be re-run for a tag on its own.

`make cross` writes version-less binaries to `dist/`, and `make package`
archives them into `dist/archives/` — a `.tar.gz` per unix platform, a `.zip`
per windows platform, and `checksums.txt`. Each archive contains a plain
`gowasm`, so extracting one gives you a binary you can run.

The two steps are separate on purpose, and `package` never rebuilds: anything
done to the binaries in between — signing, notarizing, stripping — survives.

Pushing a tag matching `v*` runs exactly these steps in GitHub Actions and
publishes the archives to a release. `.github/workflows/release.yml` gates the
build on `lint`, `test` and `test-runtime` first, so a broken binary cannot be
published, and will sign and notarize the macOS binaries when Apple credentials
are present as repository secrets — see the comments in that file. Without them
the release ships unsigned.

## Working on gowasm

```sh
make verify      # lint, unit tests, runtime tests under real wasm, all examples
make examples    # build and test every example end to end
make cross       # cross-compile for every released platform
```

`make test-runtime` compiles the runtime bridge for js/wasm and runs its tests
under the real `wasm_exec.js`, through `go_js_wasm_exec`. That is the same
source that gets emitted into generated packages, so those tests cover what
users actually run.
