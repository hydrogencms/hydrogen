# WebAssembly hooks

Hooks run code at points in an item's life: before a draft is validated or saved, before a version is published,
after it is written, around deletes. Go projects write them as Go functions (`hooks.Hook`). The **wasmhooks**
module lets teams that do not write Go, or that want to change a hook without rebuilding the server, write them in
any language that compiles to WebAssembly (Go, Rust, TinyGo, TypeScript through Javy, AssemblyScript, ...). Hooks
run in a sandbox: they get a time limit and a memory limit, and they cannot reach the file system, the environment
or the network.

```sh
hydrogen add wasmhooks
```

The module is its own Go module because it depends on [wazero](https://wazero.io) (a pure Go WebAssembly runtime,
no cgo). Projects that do not add it pay nothing.

## Configuration

`modules.wasmhooks`:

```yaml
modules:
  wasmhooks:
    dir: hooks                 # relative to the project directory
    timeout: 200ms             # per call; a hook may set its own (at most 10s)
    memory: 32MB               # per instance (at most 512MB)
    concurrency: 0             # simultaneous calls; 0 = GOMAXPROCS
    hooks:
      - { file: reading-time.wasm, event: beforeSave, types: [article] }
      - { file: no-shouting.wasm, event: validate }
      - { file: notify.wasm, event: afterPublish, async: true, name: notify }
```

| Key | Default | Meaning |
| --- | --- | --- |
| `dir` | `hooks` | Where the files live, relative to the directory the server runs in. |
| `timeout` | `200ms` | Deadline of one call, from start to the last byte of output. Waiting for a free slot counts. Maximum `10s`. |
| `memory` | `32MB` | Largest linear memory of one instance. Between `1MB` and `512MB`. A program that needs more memory than this just to start is rejected when it is compiled. |
| `concurrency` | `0` | How many calls run at once, across all hooks; `0` is `GOMAXPROCS`. Further calls wait for a slot (within their timeout). |
| `hooks[].file` | required | A file inside `dir` (a relative path, no `..`). Entries may share a file. |
| `hooks[].event` | required | One of `beforeValidate`, `validate`, `beforeSave`, `beforePublish`, `afterPublish`, `beforeDelete`, `afterDelete`, `beforeUnpublish`, `afterUnpublish`. |
| `hooks[].types` | all types | Content types the hook runs for. One hook is registered per type. |
| `hooks[].async` | `false` | Run after commit from the outbox. Only `afterPublish` and `afterDelete`; needs `name`. |
| `hooks[].name` | none | The outbox consumer name of an async hook (unique, stable: delivery progress is kept under it). With several `types` the type is appended: `notify.article`, `notify.page`. |
| `hooks[].timeout` | module's | Overrides `timeout` for this hook. |

What each event is for, and when it runs, is the same as for Go hooks. The ones that need care:

- `beforeValidate` and `beforeSave` run on draft writes. `beforeSave` runs after validation, so it is where
  computed fields belong.
- `validate` runs on draft writes and on publish; `beforePublish` runs just before a version is written.
- `afterPublish` and `afterDelete` run inside the transaction, or after commit when `async: true`. Async hooks are
  delivered at least once and retried on failure, so they must be idempotent.
- Async `afterPublish` hooks run once per published locale.

`hydrogen config:check` validates this section and compiles every file, so a missing file, a corrupt module or a
program that is not a WASI command fails there and at start-up. Types named in `types` are checked at start-up
against the schema.

## ABI v1

A hook is a **WASI preview 1 command**: a program with a `_start` function, built for `wasip1` (Go `GOOS=wasip1`,
Rust `wasm32-wasip1`, ...). Each call starts a fresh instance:

- **stdin** is the request, one JSON object.
- **stdout** is the response, one JSON object (at most 1 MB). Empty output or `{}` means "no change".
- **stderr** is logged (the first 4 KB of each call, at info level, with the hook file and event).
- **Exit code** must be 0. Non-zero, a trap or a panic fails the hook.
- The only argument (`argv[0]`) is the hook file name. The environment is empty.
- The wall clock and random numbers are available.

Nothing survives between calls: globals start from their initial values every time. Hooks that need state read it
from the document or compute it again.

### Request

```json
{ "v": 1, "event": "beforeSave", "locale": "en",
  "item": { "id": "0f8c...", "type": "article", "key": "hello", "version": 0, "mainLocale": "en" },
  "doc": { "title": "Hello", "body": "...", "readingMinutes": 1 } }
```

`doc` is the merged document of `locale` as the service sees it (shared fields plus the locale's, drafts applied).
It is `null` for delete and unpublish events. `item.version` is the published version (0 when there is none);
`key` is empty when the item has none. Numbers are plain JSON numbers: integers stay integers. `v` is the ABI
version; new keys may be added in v1, so ignore keys you do not know.

### Response

Every key is optional.

```json
{ "set":    { "readingMinutes": 4 },
  "errors": [ { "path": "title", "message": "Too long" } ],
  "error":  "Publishing is frozen" }
```

| Key | Events | Effect |
| --- | --- | --- |
| `set` | `beforeValidate`, `beforeSave` | Top-level fields to set in the document. `null` removes a field. |
| `errors` | `validate`, `beforePublish` | Field errors, shown in the editor like schema errors. Each has a `path` (`title`, `tags[1]`, `address.city`; empty for the whole document), a `message` and optionally a `code` (default `hook`). |
| `error` | `beforeValidate`, `beforeSave`, `beforePublish`, `beforeDelete`, `beforeUnpublish` | Blocks the operation. The message is returned to the caller as a 400; an empty message still blocks. |

Any other key, a key that does not belong to the event, or an `after*` event answering with anything but `{}`, is a
hook bug and fails the operation (the error names the hook file and event).

`set` rules:

- Fields must exist on the item's type, or the call fails.
- In `beforeValidate` the values go through the validation that follows, like user input.
- In `beforeSave` validation has already run, so the module checks the values you set against the schema (type,
  limits) and fails the call when they are invalid. Integral numbers for integer fields (`5.0`) are accepted.
- Setting a field the user also edits overwrites their value; that is usually what a computed field wants.

## Sandbox and limits

| Limit | How |
| --- | --- |
| Time | `timeout` per call, enforced by the runtime even for an infinite loop, and it also cuts short a sleep or a blocking system call (no cooperation needed). The caller's own cancellation (a closed request) stops the call too. |
| Memory | `memory` per instance; growing past it fails inside the program (Go reports `out of memory` and exits 2). |
| Files | No directories are mounted: opening any path fails. |
| Network | WASI preview 1 has no sockets. |
| Environment | None; `argv` holds only the file name. |
| Output | stdout at most 1 MB (more fails the call); stderr is truncated to 4 KB; stdin is the request as is. |
| Concurrency | `concurrency` calls at once. |
| Tables | A module whose tables start with more than 100000 entries is rejected when compiled. |
| Imports | A file may import only `wasi_snapshot_preview1`; anything else fails when compiled. |

Files are compiled once, when the server starts (and by `config:check`), with wazero's compiler, and kept in an
in-memory cache; every call instantiates the compiled code fresh, which costs a few milliseconds at most (a
Go program, which initialises its whole runtime on every call, takes about 2 ms on a laptop; Rust and TinyGo
start faster). Prefer small programs for hot events like `beforeSave`.

A hook sees only what it is given and cannot call back into Hydrogen. Anything that needs a database or a
network call belongs in an async hook that a Go module or a webhook consumer carries out, or in a Go hook.

## Failures

A hook that times out, traps, exits non-zero, writes invalid JSON, writes too much, or breaks the response rules
fails the operation. The error names the hook file and the event, for example

```
beforeSave hook (wasmhooks): wasm hook reading-time.wasm (beforeSave): timed out after 200ms
```

and for non-zero exits includes the tail of stderr. Synchronous hooks fail the request they run in (a draft write,
a publish, ...) and, being in its transaction, roll it back. Async hooks fail their outbox delivery and are retried
with backoff. A hook that blocks on purpose (`error`) is a client error (400), every other failure is a server
error (500).

## Development

With `APP_ENV=dev` (the default) the server stats the hook file on every call and recompiles it (at most one check per 500 ms per file) when its size or
modification time changed, so you can rebuild a hook and try it without restarting. A hook file that fails to
compile fails the calls that use it, with the compile error, until it is fixed; the previous version is not kept. Recompiling does not count toward the call's
timeout.
Outside dev files are read once, at start; replace the file and restart (or deploy a new binary and files together).

`hydrogen dev` restarts the server on Go changes only; it does not need to know about `.wasm` files. If you add a
new entry to `hooks:` in the configuration, the server restarts as it does for any configuration change.

Test a hook by running it by hand: it is just a program.

```sh
echo '{"v":1,"event":"beforeSave","locale":"en","item":{"type":"article"},"doc":{"body":"a b c"}}' \
  | wasmtime hooks/reading-time.wasm
```

## Go

Build with the standard toolchain (Go 1.21 or later). A hook is an ordinary `main` package that reads the request
and writes the response:

```go
// hooks/reading-time/main.go
package main

import (
	"encoding/json"
	"os"
	"strings"
)

type request struct {
	Doc map[string]any `json:"doc"`
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		os.Exit(1)
	}
	body, _ := req.Doc["body"].(string)
	minutes := max(1, len(strings.Fields(body))/200)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"set": map[string]any{"readingMinutes": minutes}})
}
```

```sh
GOOS=wasip1 GOARCH=wasm go build -o hooks/reading-time.wasm ./hooks/reading-time
```

A Go program is a few MB and adds about 2 ms per call. TinyGo (`tinygo build -target=wasip1`) makes much smaller,
faster-starting files for the same source.

## Rust

```toml
# Cargo.toml
[package]
name = "no-shouting"
version = "0.1.0"
edition = "2021"

[dependencies]
serde = { version = "1", features = ["derive"] }
serde_json = "1"

[profile.release]
opt-level = "s"
lto = true
```

```rust
// src/main.rs
use serde::Deserialize;
use serde_json::json;
use std::io::Read;

#[derive(Deserialize)]
struct Request {
    doc: Option<serde_json::Value>,
}

fn main() {
    let mut input = String::new();
    std::io::stdin().read_to_string(&mut input).unwrap();
    let req: Request = serde_json::from_str(&input).unwrap();
    let title = req.doc.as_ref().and_then(|d| d["title"].as_str()).unwrap_or("");
    let letters: Vec<char> = title.chars().filter(|c| c.is_alphabetic()).collect();
    if letters.len() > 3 && letters.iter().all(|c| c.is_uppercase()) {
        println!("{}", json!({ "errors": [{ "path": "title", "message": "No shouting" }] }));
    }
}
```

```sh
rustup target add wasm32-wasip1
cargo build --release --target wasm32-wasip1
cp target/wasm32-wasip1/release/no-shouting.wasm hooks/
```

## TypeScript

TypeScript hooks use the `@hydrogencms/hook` package. [esbuild](https://esbuild.github.io) bundles a hook into one
JavaScript file and [Javy](https://github.com/bytecodealliance/javy) compiles it, with its JavaScript engine, into a
WASI command. A compiled hook is about 1.3 MB, which is Javy's engine, and starts in a few milliseconds.

```sh
npm i -D esbuild @hydrogencms/hook
hydrogen generate -ts gen/types.ts   # the project's types (hydrogen dev keeps gen/ current)
```

```ts
// hooks/reading-time.ts
import { defineHook } from '@hydrogencms/hook'
import type { Article } from '../gen/types'

defineHook<Article, 'beforeSave'>((req) => ({
  set: { readingMinutes: Math.max(1, Math.round(JSON.stringify(req.doc?.body ?? '').length / 1200)) },
}))
```

`defineHook<Doc, Event>(fn)` reads the request from stdin, calls `fn` and writes the response to stdout. `Doc` is the
document type (a generated type such as `Article`, or `Record<string, unknown>`); `Event` narrows the request's
`event`. `fn` is synchronous and returns a `HookResponse` (`set`, `errors`, `error`), or nothing for no change. A thrown
error fails the call. `console.error` writes to stderr, which the server logs; do not use `console.log`, it writes to stdout and
breaks the response. The package exports the `HookRequest`, `HookResponse`, `HookEvent`,
`HookItem` and `HookFieldError` types.

Build with the CLI, then list the files in `modules.wasmhooks.hooks`:

```sh
hydrogen hooks build                 # every hooks/*.ts → hooks/<name>.wasm
hydrogen hooks build hooks/a.ts      # only these files
```

For each file `hydrogen hooks build` runs `npx --no-install esbuild <file> --bundle --format=esm --platform=neutral`
in the project and then `javy build <bundle>.js -o hooks/<name>.wasm -J javy-stream-io -J text-encoding`. The flags
switch on the two Javy features the package uses (`Javy.readSync`/`writeSync` and `TextEncoder`/`TextDecoder`); use them
too if you run `javy build` yourself. The command needs Node 20+ and esbuild in the project (`npm i -D esbuild`), and
Javy 9 on `PATH` or named by the `JAVY` environment variable. Download a release from
[github.com/bytecodealliance/javy/releases](https://github.com/bytecodealliance/javy/releases) (`javy-<arch>-<os>-v9.1.0.gz`,
unpack and `chmod +x`); this repository's `scripts/javy.sh` does so with a pinned checksum for its own tests. Rebuild after
editing a hook: with `APP_ENV=dev` the server picks up the new `.wasm` on the next call.

Bundles are limited to what Javy's engine offers (ES2023 and the Web APIs it enables): no Node built-ins, no `fetch`,
no timers. Keep hooks small and avoid heavy dependencies; each call starts the whole bundle again.
