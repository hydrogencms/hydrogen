<script lang="ts">
  import type { BlockProps } from '@hydrogencms/sveltekit/components'
  import type { CalloutProps } from '$lib/types'

  let { props: p, attrs }: BlockProps<CalloutProps> = $props()
</script>

<div {...attrs} class="container prose">
  <aside class="callout tone-{p.tone ?? 'brand'}"><p>{p.text}</p></aside>
</div>
