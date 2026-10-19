<script lang="ts">
  import type { Component } from 'svelte'
  import Live from './Live.svelte'
  import type { PageData } from './index.js'

  /**
   * Renders what `load` returned: the head (title, canonical, hreflang alternates; noindex and the token proof in
   * preview) and the template for the item's type. In preview the template renders from the admin's working document.
   * A template receives `{ item, locale, metadata, preview }`.
   */
  let { data, templates, fallback }: {
    data: PageData
    templates: Record<string, Component<any>>
    fallback?: Component<any>
  } = $props()
  const T = $derived(data.template === '*' ? fallback : templates[data.template])
  const m = $derived(data.metadata)
</script>

<svelte:head>
  <title>{m.title}</title>
  {#if m.canonical}<link rel="canonical" href={m.canonical} />{/if}
  {#each m.alternates as a (a.hreflang)}<link rel="alternate" hreflang={a.hreflang} href={a.href} />{/each}
  {#if data.preview}
    <meta name="robots" content="noindex" />
    <meta name="hydrogen-preview" content={data.previewHash ?? ''} />
  {/if}
</svelte:head>

{#if T}
  {#if data.preview}
    <Live item={data.item} adminOrigin={data.adminOrigin}>
      {#snippet children(item)}<T {item} locale={data.locale} metadata={m} preview={true} />{/snippet}
    </Live>
  {:else}
    <T item={data.item} locale={data.locale} metadata={m} preview={false} />
  {/if}
{/if}
