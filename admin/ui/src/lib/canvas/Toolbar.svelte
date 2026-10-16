<script lang="ts">
  import { onMount, type Snippet } from 'svelte';

  let {
    label,
    x,
    y,
    hidden = false,
    width = $bindable(0),
    onkeydown,
    children,
  }: {
    label: string;
    x: number;
    y: number;
    hidden?: boolean;
    width?: number;
    /** Runs first; call preventDefault to skip the toolbar's own arrow keys. */
    onkeydown?: (e: KeyboardEvent, root: HTMLElement) => void;
    children: Snippet;
  } = $props();

  let el = $state<HTMLElement>();
  let current: HTMLElement | undefined;

  export function focus() {
    el?.focus();
  }
  export function contains(n: Node | null) {
    return !!n && !!el?.contains(n);
  }

  const items = () => [...(el?.querySelectorAll<HTMLElement>('button:not(:disabled):not(.h-canvas__anchor), a[href]') ?? [])];

  // Roving tabindex: one stop in the Tab order, arrows move within.
  function rove() {
    const all = items();
    if (!current || !all.includes(current)) current = all[0];
    for (const b of all) b.tabIndex = b === current ? 0 : -1;
  }
  onMount(() => {
    rove();
    const mo = new MutationObserver(rove);
    mo.observe(el!, { childList: true, subtree: true, attributes: true, attributeFilter: ['disabled'] });
    return () => mo.disconnect();
  });

  function key(e: KeyboardEvent) {
    onkeydown?.(e, el!);
    if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return;
    const all = items();
    if (!all.length) return;
    const i = all.indexOf((e.target as HTMLElement).closest<HTMLElement>('button,a') ?? current!);
    let to: HTMLElement | undefined;
    if (e.key === 'ArrowRight') to = all[(i + 1) % all.length];
    else if (e.key === 'ArrowLeft') to = all[(i - 1 + all.length) % all.length];
    else if (e.key === 'Home') to = all[0];
    else if (e.key === 'End') to = all[all.length - 1];
    if (!to) return;
    e.preventDefault();
    current = to;
    rove();
    to.focus();
  }
  function focusin(e: FocusEvent) {
    const b = (e.target as HTMLElement).closest<HTMLElement>('button,a');
    if (b && items().includes(b)) {
      current = b;
      rove();
    }
  }
</script>

<div
  bind:this={el}
  bind:clientWidth={width}
  class="h-canvas__tb"
  role="toolbar"
  aria-label={label}
  tabindex="-1"
  {hidden}
  style:left="{x}px"
  style:top="{y}px"
  onkeydown={key}
  onfocusin={focusin}
>
  {@render children()}
</div>
