<script lang="ts">
  import type { BlockProps } from '@hydrogencms/sveltekit/components'
  import type { CardProps } from '$lib/types'
  import { getLinks } from './links'

  let { props: p, attrs }: BlockProps<CardProps> = $props()
  const href = $derived(p.link ? getLinks()[p.link] : undefined)
</script>

<article {...attrs} class="card">
  <h3>{#if href}<a {href}>{p.title}</a>{:else}{p.title}{/if}</h3>
  {#if p.text}<p>{p.text}</p>{/if}
</article>
