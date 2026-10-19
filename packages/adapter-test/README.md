# @hydrogencms/adapter-test

The conformance suite for Hydrogen frontend adapters: the definition of "works with Hydrogen". It checks pages, links,
SEO metadata, redirects, 404s, preview, share links, the preview bridge and revalidation against a running CMS and your
frontend. The checks and options are listed in [docs/adapter-kit.md](../../docs/adapter-kit.md#checking-your-adapter);
the bridge messages it plays are in [docs/bridge-protocol.md](../../docs/bridge-protocol.md).

```sh
npm i -D @hydrogencms/adapter-test
```

## Use

Load the CMS with the starter fixture (`examples/starter`, pointed at your frontend), then:

```sh
npx hydrogen-adapter-test --cms http://127.0.0.1:8080 --token <management token> --frontend http://127.0.0.1:3000 --browser
```

Options: `--only pages,preview` (groups: pages links seo notfound redirects reserved blog preview share bridge
revalidate), `--channels en,de`, `--admin-origin`, `--revalidate-timeout`, `--json`; `HYDROGEN_URL` and
`HYDROGEN_TOKEN` stand in for `--cms` and `--token`. `--browser` runs the bridge checks with your own Playwright.
The suite edits and publishes one page and one post and puts them back, so use a scratch database.

## From code

```ts
import { runSuite, format } from '@hydrogencms/adapter-test'

const report = await runSuite({ cms, token, frontend, channels: ['en', 'de'] })
console.log(format(report))
```

Also exported: `GROUPS`, `Recorder` and the helpers the suite is built from (`html`, `expectedBlocks`, `diffMultiset`).
