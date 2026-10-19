<svelte:options customElement={{ tag: 'x-banner', props: { ctx: { type: 'Object' }, type: { type: 'String' }, at: { type: 'String' } } }} />

<script lang="ts">
  import type { AdminContext } from '@hydrogencms/admin-sdk'

  let { ctx, type = '' }: { ctx: AdminContext; type?: string } = $props()
</script>

<p>Editing {type} as {ctx.principal.name}</p>

<style>
  p { margin: 0 0 var(--h-space-3); padding: var(--h-space-2) var(--h-space-3); border-radius: var(--h-radius);
    background: var(--h-accent-soft); color: var(--h-text); }
</style>
