<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import { portal } from './focus';
  import { router } from '../router.svelte';

  export interface MenuItem {
    label: string;
    icon?: string;
    /** In-app path (no prefix): rendered as a link. */
    href?: string;
    onselect?: () => void;
    danger?: boolean;
    disabled?: boolean;
    /** A divider is drawn above this item. */
    separator?: boolean;
    checked?: boolean;
    /** With `checked`: an independent on/off item (menuitemcheckbox) instead of one choice of a group. */
    toggle?: boolean;
    /** Muted text at the right (a shortcut, a count). */
    hint?: string;
    /** A non-interactive group label instead of an item. */
    heading?: boolean;
  }
  let {
    items,
    label,
    align = 'end',
    side = 'bottom',
    trigger,
    triggerClass = '',
  }: {
    items: MenuItem[];
    /** Accessible name of the trigger button. */
    label: string;
    align?: 'start' | 'end';
    /** Open below (default) or above the trigger (bottom of the sidebar). */
    side?: 'bottom' | 'top';
    /** Extra class for the trigger button. */
    triggerClass?: string;
    /** Content of the trigger button (default: a "more" icon). */
    trigger?: Snippet;
  } = $props();

  let open = $state(false);
  let btn = $state<HTMLButtonElement>();
  let list = $state<HTMLElement>();
  let pos = $state({ top: 0, left: 0 });
  let up = $state(false);
  const uid = $props.id();

  const enabled = () => [...(list?.querySelectorAll<HTMLElement>('[role^=menuitem]:not([aria-disabled=true])') ?? [])];

  function show(focusLast = false) {
    const r = btn!.getBoundingClientRect();
    up = side === 'top';
    pos = { top: up ? r.top - 4 : r.bottom + 4, left: align === 'end' ? r.right : r.left };
    open = true;
    queueMicrotask(() => {
      // Open on the other side when the menu does not fit on its own and does there.
      const h = list?.offsetHeight ?? 0;
      const below = innerHeight - r.bottom - 4, above = r.top - 4;
      if (up ? above < h && below >= h : below < h && above >= h) {
        up = !up;
        pos = { ...pos, top: up ? r.top - 4 : r.bottom + 4 };
      }
      const e = enabled();
      (focusLast ? e[e.length - 1] : e[0])?.focus();
    });
  }

  function hide(refocus = true) {
    open = false;
    if (refocus) btn?.focus();
  }

  function pick(it: MenuItem) {
    if (it.disabled) return;
    hide();
    if (it.href) void router.navigate(it.href);
    it.onselect?.();
  }

  function onkey(e: KeyboardEvent) {
    const e_ = enabled();
    const i = e_.indexOf(document.activeElement as HTMLElement);
    if (e.key === 'ArrowDown') e_[(i + 1) % e_.length]?.focus();
    else if (e.key === 'ArrowUp') e_[(i - 1 + e_.length) % e_.length]?.focus();
    else if (e.key === 'Home') e_[0]?.focus();
    else if (e.key === 'End') e_[e_.length - 1]?.focus();
    else if (e.key === 'Escape') hide();
    else if (e.key === 'Tab') hide(false);
    else return;
    if (e.key !== 'Tab') e.preventDefault();
  }

  $effect(() => {
    if (!open) return;
    const away = (e: PointerEvent) => {
      const n = e.target as Node;
      if (!list?.contains(n) && !btn?.contains(n)) hide(false);
    };
    addEventListener('pointerdown', away, true);
    return () => removeEventListener('pointerdown', away, true);
  });
</script>

<button
  bind:this={btn}
  type="button"
  class="h-menu__btn {trigger ? '' : 'h-menu__btn--icon'} {triggerClass}"
  aria-haspopup="menu"
  aria-expanded={open}
  aria-controls="{uid}-m"
  aria-label={trigger ? undefined : label}
  title={trigger ? undefined : label}
  onclick={() => (open ? hide() : show())}
  onkeydown={(e) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      show();
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      show(true);
    }
  }}
>
  {#if trigger}{@render trigger()}{:else}<Icon name="more-horizontal" />{/if}
</button>

{#if open}
  <div
    bind:this={list}
    use:portal
    id="{uid}-m"
    class="h-menu"
    class:h-menu--end={align === 'end'}
    class:h-menu--top={up}
    role="menu"
    aria-label={label}
    tabindex="-1"
    style:top="{pos.top}px"
    style:left="{pos.left}px"
    onkeydown={onkey}
  >
    {#each items as it, i (i)}
      {#if it.separator}<div class="h-menu__sep" role="separator"></div>{/if}
      {#if it.heading}
        <div class="h-menu__head" role="presentation">{it.label}</div>
      {:else}
      <button
        type="button"
        role={it.checked === undefined ? 'menuitem' : it.toggle ? 'menuitemcheckbox' : 'menuitemradio'}
        aria-checked={it.checked}
        aria-disabled={it.disabled || undefined}
        class="h-menu__item"
        class:h-menu__item--danger={it.danger}
        tabindex="-1"
        onclick={() => pick(it)}
      >
        {#if it.icon}<Icon name={it.icon} size={16} />{:else if it.checked}<Icon name="check" size={16} />{/if}
        {it.label}{#if it.hint}<span class="h-menu__hint">{it.hint}</span>{/if}
      </button>
      {/if}
    {/each}
  </div>
{/if}
