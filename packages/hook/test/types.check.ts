import { defineHook, type HookRequest } from '../dist/index.js'

interface Article { title: string; readingMinutes?: number }

export function check() {
  defineHook<Article, 'beforeSave'>((req) => {
    const r: HookRequest<Article, 'beforeSave'> = req
    return { set: { readingMinutes: (r.doc?.title ?? '').length } }
  })
  defineHook((req) => (req.doc?.['x'] ? { error: 'no' } : undefined))
  // @ts-expect-error not a field of Article
  defineHook<Article>(() => ({ set: { nope: 1 } }))
}
