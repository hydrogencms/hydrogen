<svelte:options customElement={{ tag: 'x-assets-banner', props: { ctx: { type: 'Object' }, at: { type: 'String' } } }} />

<script lang="ts">
  import type { AdminContext } from '@hydrogencms/admin-sdk'

  let { at = '' }: { ctx: AdminContext; at?: string } = $props()
</script>

<p>Media guidelines apply ({at})</p>

<style>
  p { margin: 0 0 var(--h-space-3); padding: var(--h-space-2) var(--h-space-3); border-radius: var(--h-radius);
    background: var(--h-accent-soft); color: var(--h-text); }
</style>
