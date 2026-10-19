<script lang="ts">
  import type { BlockProps } from '@hydrogencms/sveltekit/components'
  import type { CardsProps } from '$lib/types'

  let { props: p, attrs, renderSlot }: BlockProps<CardsProps> = $props()
</script>

<section {...attrs} class="container cards space-{p.space ?? 'md'}">
  {#if p.heading}<h2>{p.heading}</h2>{/if}
  <div class="grid cols-{p.columns ?? '3'}">{@render renderSlot('items')}</div>
</section>
