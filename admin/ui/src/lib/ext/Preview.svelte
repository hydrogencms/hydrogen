<script lang="ts">
  import { untrack } from 'svelte';
  import { ext } from '../extensions.svelte';
  import { getCtx, mountElement, type Mounted } from './mount';

  // A block preview element laid over the built-in rendering. The box fills its (fixed-size) parent and stays
  // transparent until the element is defined and mounted, so loading, a missing definition or a throwing element
  // all leave the built-in rendering showing, and nothing moves.
  let {
    tag,
    type,
    props,
    context,
  }: { tag: string; type: string; props: Record<string, unknown>; context: 'palette' | 'card' } = $props();

  let box: HTMLElement;
  let mounted = $state.raw<Mounted | null>(null);

  $effect(() => {
    const name = tag;
    let dead = false;
    let m: Mounted | undefined;
    void ext.ensureElements().then(() => {
      if (dead || !customElements.get(name)) return;
      untrack(() => {
        try {
          m = mountElement(box, name, { type, props, context }, { ctx: getCtx() });
          mounted = m;
        } catch (e) {
          console.warn(`block preview "${name}" failed: using the built-in one`, e);
        }
      });
    });
    return () => {
      dead = true;
      m?.destroy();
      mounted = null;
    };
  });
  // A setter that throws later drops the element too, back to the built-in rendering.
  function guard(run: (m: Mounted) => void) {
    const m = mounted;
    if (!m) return;
    try {
      run(m);
    } catch (e) {
      console.warn(`block preview "${tag}" failed: using the built-in one`, e);
      untrack(() => {
        m.destroy();
        mounted = null;
      });
    }
  }
  $effect(() => guard((m) => m.update({ type, props, context })));
  $effect(() => guard((m) => m.setCtx(getCtx())));
</script>

<div class="h-preview" class:h-preview--ready={!!mounted} data-preview={tag} data-context={context} bind:this={box} inert aria-hidden="true"></div>

<style>
  .h-preview {
    position: absolute;
    inset: 0;
    overflow: hidden;
    contain: layout paint style;
    pointer-events: none;
  }
  .h-preview--ready {
    background: var(--h-surface);
  }
  .h-preview--ready[data-context='card'] {
    background: inherit;
  }
  .h-preview > :global(*) {
    display: block;
    max-width: 100%;
    max-height: 100%;
  }
</style>
