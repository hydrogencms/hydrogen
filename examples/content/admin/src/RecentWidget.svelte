<svelte:options customElement={{ tag: 'x-recent-widget', props: { ctx: { type: 'Object' } } }} />

<script lang="ts">
  import type { AdminContext } from '@hydrogencms/admin-sdk'

  type Entry = { id: number; action: string; subject?: string; at: string }
  let { ctx }: { ctx: AdminContext } = $props()

  let entries = $state<Entry[]>([])
  let failed = $state(false)

  $effect(() => {
    let current = true
    ctx.api
      .get('/audit?limit=5')
      .then((r: { items: Entry[] }) => current && (entries = r.items))
      .catch(() => current && (failed = true))
    return () => (current = false)
  })

  const isItem = (s?: string) => !!s && /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(s)
  function open(e: MouseEvent, subject: string) {
    e.preventDefault()
    ctx.navigate('/items/' + subject)
  }
</script>

{#if failed}
  <p>The audit log is not available to your account.</p>
{:else}
  <ul>
    {#each entries as e (e.id)}
      <li>
        {#if isItem(e.subject)}
          <a href={ctx.prefix + '/items/' + e.subject} onclick={(ev) => open(ev, e.subject!)}>{e.action}</a>
        {:else}
          <span>{e.action}</span>
        {/if}
        <time datetime={e.at}>{new Date(e.at).toLocaleString()}</time>
      </li>
    {/each}
  </ul>
{/if}

<style>
  ul { margin: 0; padding: 0; list-style: none; display: grid; gap: var(--h-space-2); }
  a { color: var(--h-accent-strong); }
  time { margin-inline-start: var(--h-space-2); color: var(--h-text-muted); font-size: var(--h-text-xs); }
</style>
