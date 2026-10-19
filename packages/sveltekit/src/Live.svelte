<script lang="ts">
  import { onMount } from 'svelte'
  import type { Snippet } from 'svelte'
  import { createBridge } from '@hydrogencms/bridge'
  import type { Bridge } from '@hydrogencms/bridge'
  import { liveDoc } from '@hydrogencms/bridge/merge'

  /** Preview: renders `children` from the admin's working document (`item` until the admin connects), posting `rendered` after each update. */
  let { item, adminOrigin, children }: { item: any; adminOrigin: string; children: Snippet<[any]> } = $props()
  let live = $state.raw<{ doc: unknown; rev: number } | null>(null)
  let bridge: Bridge | undefined
  const shown = $derived(live ? liveDoc(item, live.doc) : item)

  onMount(() => {
    bridge = createBridge({
      adminOrigin,
      mode: 'live',
      onInit: (m) => (live = { doc: m.doc, rev: m.rev }),
      onPatch: (doc, p) => (live = { doc, rev: p.rev }),
    })
    bridge.connect()
    return () => bridge?.disconnect()
  })
  $effect(() => {
    if (live) bridge?.rendered(live.rev)
  })
</script>

{@render children(shown)}
