<svelte:options customElement={{ tag: 'x-import-tool', props: { ctx: { type: 'Object' } } }} />

<script lang="ts">
  import type { AdminContext } from '@hydrogencms/admin-sdk'

  let { ctx }: { ctx: AdminContext } = $props()
  let title = $state('')
  let summary = $state('')
  let busy = $state(false)
  let error = $state('')

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    busy = true
    error = ''
    try {
      const item = await ctx.api.post('/collections/article/items', { locale: 'en', fields: { title, summary } })
      ctx.toast('success', 'Article imported')
      ctx.navigate('/items/' + item.id)
    } catch (err) {
      error = (err as { message?: string }).message ?? 'Import failed'
    } finally {
      busy = false
    }
  }
</script>

<form onsubmit={submit}>
  <h2>Import an article</h2>
  <label>Title <input name="title" required bind:value={title} /></label>
  <label>Summary <textarea name="summary" bind:value={summary}></textarea></label>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <button type="submit" class="primary" disabled={busy}>Import</button>
</form>

<style>
  form { display: grid; gap: var(--h-space-3); max-inline-size: 32rem; }
  label { display: grid; gap: var(--h-space-1); font-weight: 500; }
  h2 { margin: 0; font-size: var(--h-text-lg); }
  .error { margin: 0; color: var(--h-danger); }

  button { font: inherit; padding: var(--h-space-2) var(--h-space-4); border: 1px solid var(--h-border-strong); border-radius: var(--h-radius);
    background: var(--h-surface); color: var(--h-text); cursor: pointer; }
  button:hover { background: var(--h-surface-hover); }
  button.primary { background: var(--h-accent); border-color: var(--h-accent); color: var(--h-accent-contrast); }
  input, textarea { font: inherit; padding: var(--h-space-2) var(--h-space-3); border: 1px solid var(--h-border-strong); border-radius: var(--h-radius);
    background: var(--h-surface); color: var(--h-text); }
</style>
