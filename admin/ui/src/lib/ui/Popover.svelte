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
  let above = $state(false);
  const uid = $props.id();

  function show() {
    const r = btn!.getBoundingClientRect();
    pos = { top: r.bottom + 4, left: align === 'end' ? r.right : r.left };
    above = false;
    open = true;
  }
  function hide(refocus = true) {
    open = false;
    if (refocus) btn?.focus();
  }

  // Keep the panel on screen: above the trigger when it does not fit below, and inside the window sideways.
  $effect(() => {
    if (!open || !box || !btn) return;
    const r = btn.getBoundingClientRect();
    const b = box.getBoundingClientRect();
    let top = r.bottom + 4;
    if (top + b.height > innerHeight - 8) top = Math.max(8, r.top - b.height - 4 > 8 ? r.top - b.height - 4 : innerHeight - b.height - 8);
    const anchor = align === 'end' ? r.right : r.left;
    const left = align === 'end' ? Math.max(b.width + 8, Math.min(anchor, innerWidth - 8)) : Math.max(8, Math.min(anchor, innerWidth - b.width - 8));
    if (top !== pos.top || left !== pos.left) pos = { top, left };
    above = top < r.top;
  });

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
    class:h-pop--above={above}
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
