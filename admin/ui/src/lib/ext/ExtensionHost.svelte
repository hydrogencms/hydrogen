<script lang="ts">
  import { untrack } from 'svelte';
  import { ext } from '../extensions.svelte';
  import { getCtx, mountElement, type Mounted } from './mount';

  let {
    tag,
    props = {},
    onEvents = {},
  }: { tag: string; props?: Record<string, unknown>; onEvents?: Record<string, (e: CustomEvent) => void> } = $props();

  let host: HTMLElement;
  let mounted = $state.raw<Mounted | null>(null);

  // The element module loads first, so the element is created already defined (no upgrade after props are set).
  $effect(() => {
    const name = tag;
    let dead = false;
    let m: Mounted | undefined;
    void ext.ensureElements().then(() => {
      if (dead) return;
      untrack(() => {
        const on: Record<string, (e: CustomEvent) => void> = {};
        for (const k of Object.keys(onEvents)) on[k] = (e) => onEvents[k]?.(e);
        m = mountElement(host, name, props, { ctx: getCtx(), on });
        mounted = m;
      });
    });
    return () => {
      dead = true;
      m?.destroy();
      mounted = null;
    };
  });
  $effect(() => mounted?.update(props));
  $effect(() => mounted?.setCtx(getCtx()));
</script>

<div class="h-ext" data-extension={tag} bind:this={host}></div>

<style>
  .h-ext {
    min-width: 0;
  }
</style>
