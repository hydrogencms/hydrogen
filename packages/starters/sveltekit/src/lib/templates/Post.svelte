<script lang="ts">
  import { Field } from '@hydrogencms/sveltekit/components'
  import type { Item } from '@hydrogencms/sveltekit'
  import type { Post } from '$lib/types'
  import Body from '$lib/Body.svelte'

  let { item, locale, preview }: { item: Item<Post>; locale: string; preview: boolean } = $props()
  const date = $derived(new Date(item.publishedAt))
</script>

<Body {item} {locale} {preview}>
  {#snippet lead()}
    <p class="meta">
      <time datetime={item.publishedAt}>{isNaN(+date) ? '' : date.toLocaleDateString(locale, { dateStyle: 'long', timeZone: 'UTC' })}</time>
      {#if item.fields.excerpt}
        · <Field path="fields/excerpt" {preview} value={item.fields.excerpt} />
      {/if}
    </p>
  {/snippet}
</Body>
