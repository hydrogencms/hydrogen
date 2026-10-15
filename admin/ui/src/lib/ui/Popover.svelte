<script lang="ts">
  import type { Snippet } from 'svelte';
  import { portal, trapTab } from './focus';

  let {
    label,
    trigger,
    children,
    align = 'start',
    triggerClass = '',
    open = $bindable(false),
  }: {
    /** Accessible name of the trigger button and the popover. */
    label: string;
    /** Content of the trigger button. */
    trigger: Snippet;
    /** The popover body; receives `close()`. */
    children: Snippet<[() => void]>;
    align?: 'start' | 'end';
    triggerClass?: string;
    open?: boolean;
  } = $props();

  let btn = $state<HTMLButtonElement>();
  let box = $state<HTMLElement>();
  let pos = $state({ top: 0, left: 0 });
  const uid = $props.id();

  function show() {
    const r = btn!.getBoundingClientRect();
    pos = { top: r.bottom + 4, left: align === 'end' ? r.right : r.left };
    open = true;
  }
  function hide(refocus = true) {
    open = false;
    if (refocus) btn?.focus();
  }

  $effect(() => {
    if (!open) return;
    queueMicrotask(() => box?.querySelector<HTMLElement>('input,select,button,a[href]')?.focus());
    const away = (e: PointerEvent) => {
      const n = e.target as Node;
      if (!box?.contains(n) && !btn?.contains(n)) hide(false);
    };
    addEventListener('pointerdown', away, true);
    return () => removeEventListener('pointerdown', away, true);
  });
</script>

<button bind:this={btn} type="button" class="h-popbtn {triggerClass}" aria-haspopup="dialog" aria-expanded={open} aria-controls="{uid}-p" onclick={() => (open ? hide() : show())}>
  {@render trigger()}
</button>

{#if open}
  <div
    bind:this={box}
    use:portal
    id="{uid}-p"
    class="h-pop"
    class:h-pop--end={align === 'end'}
    role="dialog"
    aria-label={label}
    tabindex="-1"
    style:top="{pos.top}px"
    style:left="{pos.left}px"
    onkeydown={(e) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        hide();
      } else if (box) trapTab(e, box);
    }}
  >
    {@render children(() => hide())}
  </div>
{/if}
