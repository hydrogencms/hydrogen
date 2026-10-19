<script lang="ts">
  import type { Snippet } from 'svelte'
  import { Blocks, Field } from '@hydrogencms/sveltekit/components'
  import { setLinks } from '$lib/blocks/links'
  import { registry } from '$lib/blocks/registry'
  import { channels } from '$lib/channels'
  import type { Block } from '$lib/types'

  /** The shared layout of the templates: title, optional intro and the body blocks. */
  let { item, locale, preview, hidden = false, lead, alternates }: {
    item: { links?: Record<string, string>; fields: { title: string; body?: Block[] } }
    locale: string
    preview: boolean
    /** the page opens with a hero that carries the visible heading */
    hidden?: boolean
    lead?: Snippet
    alternates?: { hreflang: string; href: string }[]
  } = $props()

  setLinks(() => item.links ?? {})

  /** The same page in the other languages, from the route's alternates, in the channels' configuration order. */
  const order = (l: string) => (channels.includes(l) ? channels.indexOf(l) : channels.length)
  const langs = $derived([...(alternates ?? [])].sort((a, b) => order(a.hreflang) - order(b.hreflang)))
</script>

{#if langs.length >= 2}
  <nav class="container langs" aria-label="Language">
    {#each langs as a (a.hreflang)}
      <a href={new URL(a.href).pathname} hreflang={a.hreflang} aria-current={a.hreflang === locale ? 'true' : undefined}>{a.hreflang.toUpperCase()}</a>
    {/each}
  </nav>
{/if}
<article lang={locale}>
  <header class={hidden ? 'sr-only' : 'page-head'}>
    <div class="container">
      <h1><Field path="fields/title" {preview} value={item.fields.title} /></h1>
      {@render lead?.()}
    </div>
  </header>
  <Blocks blocks={item.fields.body} {registry} path="fields/body" {preview} />
</article>
