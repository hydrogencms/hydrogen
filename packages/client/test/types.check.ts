import { createClient, linkTo, type Item } from '../dist/index.js'
import type { HydrogenTypes, Article, Landing } from './fixtures/types.js'

type Eq<A, B> = (<X>() => X extends A ? 1 : 2) extends <X>() => X extends B ? 1 : 2 ? true : false
const ok = <T extends true>(): T | undefined => undefined

const c = createClient<HydrogenTypes>({ baseUrl: 'https://x.test', channel: 'web' })

export async function check() {
  const a = await c.item<'article'>('id')
  ok<Eq<typeof a.data, Item<Article>>>()
  const s = await c.singleton('landing')
  ok<Eq<typeof s.data.fields, Landing>>()
  const l = await c.list('article', { where: { title: { contains: 'x' }, slug: 'a' }, sort: ['-title'], limit: 5 })
  ok<Eq<typeof l.data.items[number]['fields'], Article>>()
  const t: string = l.data.items[0]!.fields.title
  void t
  // @ts-expect-error unknown content type
  await c.list('nope')
  // @ts-expect-error unknown content type
  await c.item<'nope'>('id')
  // @ts-expect-error bad operator
  await c.list('article', { where: { title: { like: 'x' } } })
  const u: string | undefined = linkTo(a.data, 'x')
  void u
  const pc = c.withPreview('tok')
  ok<Eq<typeof pc, typeof c>>()
}
