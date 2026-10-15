<svelte:options customElement={{ tag: 'x-brand-color', props: {
  ctx: { type: 'Object' }, value: { type: 'String' }, field: { type: 'Object' }, readonly: { type: 'Boolean' }, error: { type: 'String' },
} }} />

<script lang="ts">
  import { emit, type AdminContext } from '@hydrogencms/admin-sdk'

  let { ctx, value = '', readonly = false, error = '' }: {
    ctx: AdminContext; value?: string | null; readonly?: boolean; error?: string
  } = $props()
  const el = $host()

  const hex = $derived(/^#[0-9a-fA-F]{6}$/.test(value ?? '') ? (value as string) : '#000000')
  const set = (next: string) => emit(el, 'change', { value: next })
</script>

<div class="row">
  <input type="color" class="picker" aria-label={ctx.t('brand.color')} value={hex} disabled={readonly}
    oninput={(e) => set(e.currentTarget.value)} />
  <input type="text" class="hex" aria-label={ctx.t('brand.hex')} placeholder="#rrggbb" value={value ?? ''}
    {readonly} aria-invalid={error ? 'true' : undefined} oninput={(e) => set(e.currentTarget.value)} />
</div>
{#if error}<p class="error" role="alert">{error}</p>{/if}

<style>
  .row { display: flex; gap: var(--h-space-2); align-items: center; }
  .picker { inline-size: 2.5rem; block-size: 2.25rem; padding: 0; }
  .hex { max-inline-size: 10rem; font-family: var(--h-mono); }
  .hex[aria-invalid='true'] { border-color: var(--h-danger); }
  .error { margin: var(--h-space-1) 0 0; color: var(--h-danger); font-size: var(--h-text-sm); }

  input { font: inherit; padding: var(--h-space-2) var(--h-space-3); border: 1px solid var(--h-border-strong); border-radius: var(--h-radius);
    background: var(--h-surface); color: var(--h-text); }
</style>
