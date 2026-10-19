<script lang="ts">
  import type { BlockProps } from '@hydrogencms/sveltekit/components'
  import type { ImageProps } from '$lib/types'

  let { props: p, attrs }: BlockProps<ImageProps> = $props()
</script>

<div {...attrs} class="container prose">
  <figure class="figure">
    <img src={p.src} alt={p.alt ?? ''} loading="lazy" />
    {#if p.caption}<figcaption>{p.caption}</figcaption>{/if}
  </figure>
</div>
