<svelte:options customElement={{ tag: 'x-export-csv', props: {
  ctx: { type: 'Object' }, type: { type: 'String' }, selected: { type: 'Array' }, query: { type: 'Object' },
} }} />

<script lang="ts">
  import { emit, type AdminContext } from '@hydrogencms/admin-sdk'

  let { ctx, type = '', selected = [] }: { ctx: AdminContext; type?: string; selected?: string[]; query?: Record<string, string> } = $props()
  const el = $host()

  const count = $derived(selected?.length ?? 0)
  function run() {
    ctx.toast('success', `Exported ${count} ${type} items`)
    emit(el, 'refresh')
  }
</script>

<button type="button" onclick={run}>Export CSV (<span data-testid="export-count">{count}</span> selected)</button>

<style>
  button { font: inherit; padding: var(--h-space-2) var(--h-space-4); border: 1px solid var(--h-border-strong); border-radius: var(--h-radius);
    background: var(--h-surface); color: var(--h-text); cursor: pointer; }
  button:hover { background: var(--h-surface-hover); }
</style>
