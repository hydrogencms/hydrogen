<script lang="ts">
  import type { BlockProps } from '@hydrogencms/sveltekit/components'
  import type { HeroProps } from '$lib/types'
  import { getLinks } from './links'

  /** Every component spreads `attrs` on its root: `data-h-block` always, `data-h-path` in preview. */
  let { props: p, attrs }: BlockProps<HeroProps> = $props()
  const href = $derived(p.ctaLink ? getLinks()[p.ctaLink] : p.ctaUrl)
</script>

<section {...attrs} class="hero tone-{p.tone ?? 'light'}">
  <div class="container">
    <h2 class="hero-title">{p.heading}</h2>
    {#if p.subheading}<p class="hero-sub">{p.subheading}</p>{/if}
    {#if p.ctaLabel && href}<a class="button" {href}>{p.ctaLabel}</a>{/if}
  </div>
</section>
