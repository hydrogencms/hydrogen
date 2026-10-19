<script lang="ts">
  import type { Item, RouteMetadata } from '@hydrogencms/sveltekit'
  import type { Landing } from '$lib/types'
  import Body from '$lib/Body.svelte'

  let { item, locale, preview, metadata }: { item: Item<Landing>; locale: string; preview: boolean; metadata: RouteMetadata } = $props()
</script>

<Body {item} {locale} {preview} alternates={metadata.alternates} hidden={item.fields.body?.[0]?.type === 'hero'} />
