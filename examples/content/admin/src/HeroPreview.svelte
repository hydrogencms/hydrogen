<svelte:options customElement={{ tag: 'x-hero-preview', props: {
  ctx: { type: 'Object' }, type: { type: 'String' }, props: { type: 'Object' }, context: { type: 'String' },
} }} />

<script lang="ts">
  import type { AdminContext } from '@hydrogencms/admin-sdk'

  let { props = {}, context = 'card' }: {
    ctx: AdminContext; type?: string; props?: { heading?: string; color?: string }; context?: 'palette' | 'card'
  } = $props()

  const color = $derived(/^#[0-9a-fA-F]{6}$/.test(props?.color ?? '') ? (props.color as string) : '#cccccc')
</script>

<span class="hero" data-context={context}>
  <span class="swatch" data-testid="hero-swatch" style:background={color}></span>
  <strong data-testid="hero-heading">{props?.heading || 'Hero'}</strong>
</span>

<style>
  .hero { display: inline-flex; align-items: center; gap: var(--h-space-2); max-inline-size: 100%; overflow: hidden; }
  .hero[data-context='palette'] { flex-direction: column; justify-content: center; inline-size: 100%; block-size: 100%; }
  .swatch { inline-size: 1rem; block-size: 1rem; border-radius: 50%; border: 1px solid var(--h-border-strong); flex: none; }
  .hero[data-context='palette'] .swatch { inline-size: 2rem; block-size: 2rem; }
  strong { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
