<svelte:options customElement={{ tag: 'x-alt-check', props: {
  ctx: { type: 'Object' }, item: { type: 'Object' }, type: { type: 'String' }, locale: { type: 'String' }, draft: { type: 'Boolean' },
} }} />

<script lang="ts">
  import type { AdminContext } from '@hydrogencms/admin-sdk'

  let { item }: { ctx: AdminContext; item?: { fields?: Record<string, unknown> }; type?: string; locale?: string; draft?: boolean } = $props()
  const alt = $derived(typeof item?.fields?.alt === 'string' ? item.fields.alt.trim() : '')
</script>

<p data-testid="alt-state">{alt ? 'Alt text is set.' : 'Alt text is missing.'}</p>

<style>
  p { margin: 0; font-size: var(--h-text-sm); }
</style>
