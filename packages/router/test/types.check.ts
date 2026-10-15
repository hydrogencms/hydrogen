import { createClient } from '@hydrogencms/client'
import { createRouter, matchChannel, type Resolved } from '../dist/index.js'

type Types = { article: { title: string } }
const router = createRouter<Types>({ client: createClient<Types>({ baseUrl: 'https://x.test' }), templates: { article: 'article' } })

export async function check() {
  const r: Resolved = await router.resolve('/a')
  if (r.kind === 'page') {
    const t: string = r.template
    const h: string = r.metadata.alternates[0]!.hreflang
    void t, h
  }
  // @ts-expect-error reserved has no tags
  r.kind === 'reserved' && r.tags
  const names: string[] = router.templates()
  void names, matchChannel
}
