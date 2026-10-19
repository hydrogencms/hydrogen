# @hydrogencms/router

Framework-neutral URL resolver for Hydrogen sites. The CMS decides which page lives at which URL; the router asks it
(`/route`) and turns the answer into something a frontend can render: a template key with the item and its metadata,
a redirect, a not found, or a path the frontend owns. The framework adapters use it; see
[docs/adapter-kit.md](../../docs/adapter-kit.md).

```sh
npm i @hydrogencms/router @hydrogencms/client
```

## Use

```ts
import { createClient } from '@hydrogencms/client'
import { createRouter } from '@hydrogencms/router'

const router = createRouter({
  client: createClient({ baseUrl: 'https://cms.acme.com', channel: 'web_en' }),
  templates: { page: 'page', post: 'post' },   // content type -> your template key
  fallback: 'generic',                         // for other types; without it they are not found
  reserved: ['/api/*'],                        // paths your frontend owns
})

const r = await router.resolve('/en/about', { preview: token, headers })
switch (r.kind) {
  case 'page': render(r.template, r.item, r.metadata); break   // metadata: title, canonical, hreflang alternates
  case 'redirect': redirect(r.location, r.status); break
  case 'notFound': notFound(); break
  case 'reserved': /* not a site path */ break
}
```

Results carry the cache `tags` for revalidation. `router.templates()` lists the declared types, which
`hydrogen check-frontend -templates` compares with the schema.

Also exported: `matchSite(url, channels, headers?)` and `matchChannel(...)`, which pick the channel a request belongs
to from its host, prefix or header matcher and return the site path.
