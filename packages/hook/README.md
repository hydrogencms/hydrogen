# @hydrogencms/hook

Write Hydrogen WebAssembly hooks in TypeScript. A hook is compiled to a sandboxed WebAssembly program with Javy and runs
inside the server on content events. Types only plus one function: no runtime dependencies. The module that runs hooks is
described in [docs/wasm-hooks.md](../../docs/wasm-hooks.md).

```sh
npm i -D @hydrogencms/hook
```

## Use

```ts
import { defineHook } from '@hydrogencms/hook'

type Article = { body?: string; readingMinutes?: number }

defineHook<Article, 'beforeSave'>((req) => {
  const words = (req.doc?.body ?? '').split(/\s+/).length
  return { set: { readingMinutes: Math.max(1, Math.round(words / 200)) } }
})
```

`defineHook(fn)` reads the request from stdin (`event`, `locale`, `item`, `doc`), calls `fn` and writes its response.
The function must be synchronous. The response may hold `set` (change fields; `beforeValidate`, `beforeSave`),
`errors` (field errors; `validate`, `beforePublish`) or `error` (block any `before*` event); return nothing for no
change. Exported types: `HookEvent`, `HookRequest`, `HookResponse`, `HookFieldError`, `HookItem`, `HookFn`.

Build and register hooks with `hydrogen hooks build`, as the wasm hooks page describes.
