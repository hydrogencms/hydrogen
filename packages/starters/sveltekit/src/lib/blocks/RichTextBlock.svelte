<script lang="ts">
  import { RichText } from '@hydrogencms/sveltekit/components'
  import type { BlockProps } from '@hydrogencms/sveltekit/components'
  import type { RichTextProps } from '$lib/types'
  import { getLinks } from './links'

  let { props: p, attrs }: BlockProps<RichTextProps> = $props()
</script>

<div {...attrs} class="container prose">
  <RichText doc={p.body} links={getLinks()} />
</div>
