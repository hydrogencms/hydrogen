<svelte:options customElement={{ tag: 'x-seo-panel', props: {
  ctx: { type: 'Object' }, item: { type: 'Object' }, type: { type: 'String' }, locale: { type: 'String' }, draft: { type: 'Boolean' },
} }} />

<script lang="ts">
  import { emit, type AdminContext } from '@hydrogencms/admin-sdk'

  type Fields = Record<string, unknown>
  let { ctx, item, locale = 'en' }: {
    ctx: AdminContext; item?: { id: string; fields: Fields; updatedAt?: string }; type?: string; locale?: string; draft?: boolean
  } = $props()
  const el = $host()

  const text = (f: unknown) => (typeof f === 'string' ? f : '')
  const title = $derived(text(item?.fields.title))
  const intro = $derived(text(item?.fields.summary ?? item?.fields.intro))
  const score = $derived(
    (title.length >= 30 && title.length <= 60 ? 50 : title.length > 0 ? 25 : 0) +
      (intro.length >= 80 && intro.length <= 160 ? 50 : intro.length > 0 ? 25 : 0),
  )

  let view = $state<'loading' | 'same' | 'differs' | 'unpublished'>('loading')
  let nonce = $state(0)

  $effect(() => {
    void nonce
    void item?.updatedAt
    const id = item?.id
    if (!id) return
    const mine = JSON.stringify(item?.fields ?? {})
    let current = true
    view = 'loading'
    ctx.api
      .get('/items/' + id + '?locale=' + encodeURIComponent(locale))
      .then((pub: { fields: Fields }) => current && (view = JSON.stringify(pub.fields) === mine ? 'same' : 'differs'))
      .catch(() => current && (view = 'unpublished'))
    return () => (current = false)
  })

  const labels: Record<typeof view, string> = { loading: '…', same: 'published', differs: 'draft differs', unpublished: 'not published' }
  const status = $derived(labels[view])
</script>

<section>
  <p>SEO score: <strong data-testid="seo-score">{score}</strong> / 100</p>
  <p>Title {title.length} characters (30 to 60), summary {intro.length} (80 to 160).</p>
  <p data-testid="seo-state">{status}</p>
  <button type="button" onclick={() => { nonce++; emit(el, 'refresh') }}>Refresh</button>
</section>

<style>
  section { display: grid; gap: var(--h-space-2); font-size: var(--h-text-sm); }
  p { margin: 0; }

  button { font: inherit; padding: var(--h-space-2) var(--h-space-4); border: 1px solid var(--h-border-strong); border-radius: var(--h-radius);
    background: var(--h-surface); color: var(--h-text); cursor: pointer; }
  button:hover { background: var(--h-surface-hover); }
</style>
