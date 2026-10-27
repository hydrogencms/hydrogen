<script lang="ts">
  import type { HTMLButtonAttributes } from 'svelte/elements';
  import Icon from './Icon.svelte';
  import { portal } from './focus';

  export interface SelectOption {
    value: string;
    label: string;
    disabled?: boolean;
  }
  type Props = Omit<HTMLButtonAttributes, 'value' | 'children' | 'onchange' | 'oninput' | 'name'> & {
    value?: string;
    options: SelectOption[];
    /** Adds a first empty option with this label; the trigger shows it, muted, while nothing is chosen. */
    placeholder?: string;
    invalid?: boolean;
    /** A small pill select (32 px) for toolbars and table footers. */
    compact?: boolean;
    /** Submitted through a hidden input when inside a form. */
    name?: string;
    /** Called with the new value after the user picks one (bind:value also updates). */
    onchange?: (value: string) => void;
  };

  let {
    value = $bindable(''),
    options,
    placeholder,
    invalid = false,
    compact = false,
    disabled = false,
    name,
    onchange,
    class: cls = '',
    ...rest
  }: Props = $props();

  const uid = $props.id();
  let open = $state(false);
  let active = $state(-1);
  let btn = $state<HTMLButtonElement>();
  let list = $state<HTMLElement>();
  let pos = $state({ top: 0, left: 0, width: 0 });
  let up = $state(false);
  let typed = '';
  let typedAt = 0;

  const all = $derived<SelectOption[]>(placeholder === undefined ? options : [{ value: '', label: placeholder }, ...options]);
  const current = $derived(options.find((o) => o.value === value));
  const shown = $derived(current?.label ?? (value === '' ? placeholder : undefined) ?? '');
  const muted = $derived(current === undefined);

  const usable = (i: number) => i >= 0 && i < all.length && !all[i].disabled;
  function step(from: number, dir: 1 | -1) {
    for (let i = from + dir; i >= 0 && i < all.length; i += dir) if (usable(i)) return i;
    return from;
  }

  function show(at?: 'first' | 'last') {
    if (disabled || open) return;
    const r = btn!.getBoundingClientRect();
    pos = { top: r.bottom + 4, left: r.left, width: r.width };
    up = false;
    const sel = all.findIndex((o) => o.value === value && !o.disabled);
    active = at === 'first' ? step(-1, 1) : at === 'last' ? step(all.length, -1) : sel >= 0 ? sel : step(-1, 1);
    open = true;
  }
  function hide() {
    open = false;
    active = -1;
  }
  function choose(i: number) {
    if (!usable(i)) return;
    const v = all[i].value;
    hide();
    btn?.focus();
    if (v !== value) {
      value = v;
      onchange?.(v);
    }
  }

  // Place the list under the trigger, or above it when it does not fit; keep it inside the window.
  $effect(() => {
    if (!open || !list || !btn) return;
    const r = btn.getBoundingClientRect();
    const h = list.offsetHeight;
    const flip = r.bottom + 4 + h > innerHeight - 8 && r.top - 4 - h >= 8;
    const left = Math.max(8, Math.min(r.left, innerWidth - Math.max(r.width, list.offsetWidth) - 8));
    const top = flip ? r.top - 4 : Math.min(r.bottom + 4, Math.max(8, innerHeight - h - 8));
    if (top !== pos.top || left !== pos.left) pos = { ...pos, top, left };
    up = flip;
  });

  $effect(() => {
    if (!open || !list || active < 0) return;
    document.getElementById(`${uid}-o${active}`)?.scrollIntoView?.({ block: 'nearest' });
  });

  $effect(() => {
    if (!open) return;
    const away = (e: PointerEvent) => {
      const n = e.target as Node;
      if (!list?.contains(n) && !btn?.contains(n)) hide();
    };
    const resize = () => hide();
    // The list is fixed to where the trigger was: scrolling anything but the list itself closes it.
    const scroll = (e: Event) => {
      if (!list?.contains(e.target as Node)) hide();
    };
    addEventListener('pointerdown', away, true);
    addEventListener('resize', resize);
    addEventListener('scroll', scroll, true);
    return () => {
      removeEventListener('pointerdown', away, true);
      removeEventListener('resize', resize);
      removeEventListener('scroll', scroll, true);
    };
  });

  function typeahead(ch: string) {
    const now = Date.now();
    typed = now - typedAt > 500 ? ch : typed + ch;
    typedAt = now;
    const lower = typed.toLowerCase();
    // Repeating one letter cycles through the options that start with it.
    const cycle = [...lower].every((c) => c === lower[0]);
    const needle = cycle ? lower[0] : lower;
    const from = active < 0 ? 0 : cycle && typed.length > 1 ? active + 1 : active;
    for (let n = 0; n < all.length; n++) {
      const i = (from + n) % all.length;
      if (usable(i) && all[i].label.toLowerCase().startsWith(needle)) {
        active = i;
        return;
      }
    }
  }

  function onkey(e: KeyboardEvent) {
    if (disabled || e.altKey || e.ctrlKey || e.metaKey) return;
    const k = e.key;
    if (!open) {
      if (k === 'Enter' || k === ' ' || k === 'ArrowDown' || k === 'ArrowUp') show();
      else if (k === 'Home') show('first');
      else if (k === 'End') show('last');
      else if (k.length === 1) {
        show();
        typeahead(k);
      } else return;
      e.preventDefault();
      return;
    }
    if (k === 'ArrowDown') active = step(active, 1);
    else if (k === 'ArrowUp') active = step(active, -1);
    else if (k === 'Home') active = step(-1, 1);
    else if (k === 'End') active = step(all.length, -1);
    else if (k === 'Enter' || k === ' ') choose(active);
    else if (k === 'Escape') {
      e.stopPropagation();
      hide();
    } else if (k === 'Tab') {
      hide();
      return;
    } else if (k.length === 1) typeahead(k);
    else return;
    e.preventDefault();
  }
</script>

<span class="h-selectwrap {cls}" class:h-selectwrap--sm={compact}>
  <button
    bind:this={btn}
    type="button"
    class="h-input h-select"
    class:h-select--empty={muted}
    role="combobox"
    aria-haspopup="listbox"
    aria-expanded={open}
    aria-controls="{uid}-l"
    aria-activedescendant={open && active >= 0 ? `${uid}-o${active}` : undefined}
    aria-invalid={invalid || undefined}
    {disabled}
    {...rest}
    onclick={() => (open ? hide() : show())}
    onkeydown={onkey}
  >
    <span class="h-select__text">{shown || ' '}</span>
  </button>
  <Icon name="chevron-down" size={16} />
  {#if name}<input type="hidden" {name} {value} />{/if}
</span>

{#if open}
  <div
    bind:this={list}
    use:portal
    id="{uid}-l"
    class="h-menu h-select__list"
    class:h-menu--top={up}
    role="listbox"
    aria-label={rest['aria-label']}
    aria-labelledby={rest['aria-labelledby']}
    tabindex="-1"
    onpointerdown={(e) => e.preventDefault()}
    style:top="{pos.top}px"
    style:left="{pos.left}px"
    style:min-width="{pos.width}px"
  >
    {#each all as o, i (i)}
      <div
        id="{uid}-o{i}"
        role="option"
        tabindex="-1"
        class="h-menu__item h-select__opt"
        class:h-select__opt--active={i === active}
        aria-selected={o.value === value}
        aria-disabled={o.disabled || undefined}
        onpointermove={() => usable(i) && active !== i && (active = i)}
        onclick={() => choose(i)}
        onkeydown={() => {}}
      >
        <span class="h-select__text" class:h-muted={i === 0 && placeholder !== undefined}>{o.label || ' '}</span>
        {#if o.value === value}<Icon name="check" size={16} />{/if}
      </div>
    {/each}
  </div>
{/if}
