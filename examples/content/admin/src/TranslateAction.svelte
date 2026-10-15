<svelte:options customElement={{ tag: 'x-translate-action', props: {
  ctx: { type: 'Object' }, item: { type: 'Object' }, type: { type: 'String' }, locale: { type: 'String' },
} }} />

<script lang="ts">
  import { emit, type AdminContext } from '@hydrogencms/admin-sdk'

  let { ctx, item }: { ctx: AdminContext; item?: { name?: string } } = $props()
  const el = $host()

  function send() {
    ctx.toast('success', `Sent "${item?.name ?? 'item'}" to translation`)
    emit(el, 'refresh')
  }
</script>

<button type="button" onclick={send}>Send to translation</button>

<style>
  button { font: inherit; padding: var(--h-space-2) var(--h-space-4); border: 1px solid var(--h-border-strong); border-radius: var(--h-radius);
    background: var(--h-surface); color: var(--h-text); cursor: pointer; }
  button:hover { background: var(--h-surface-hover); }
</style>
