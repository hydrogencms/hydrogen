<script lang="ts">
  import Icon from './Icon.svelte';
  import { t } from '../i18n';

  export interface ComboOption {
    value: string;
    label: string;
    /** Secondary text (e.g. status). */
    hint?: string;
  }
  let {
    value = $bindable(null),
    search,
    placeholder = '',
    disabled = false,
    invalid = false,
    clearable = true,
    debounce = 200,
    minChars = 0,
    onselect,
    ...rest
  }: {
    /** The selected option (label shown in the box). */
    value?: ComboOption | null;
    /** Called with the typed text; stale responses are dropped and the signal aborted. */
    search: (query: string, signal: AbortSignal) => Promise<ComboOption[]>;
    placeholder?: string;
    disabled?: boolean;
    invalid?: boolean;
    clearable?: boolean;
    debounce?: number;
    minChars?: number;
    onselect?: (o: ComboOption | null) => void;
    id?: string;
    'aria-describedby'?: string;
    'aria-invalid'?: boolean;
  } = $props();

  const uid = $props.id();
  const listId = `${uid}-l`;
  let text = $state('');
  let editing = $state(false);
  let open = $state(false);
  let options = $state<ComboOption[]>([]);
  let busy = $state(false);
  let active = $state(-1);
  let timer: ReturnType<typeof setTimeout> | undefined;
  let ctl: AbortController | undefined;
  let input = $state<HTMLInputElement>();

  const shown = $derived(editing ? text : (value?.label ?? ''));

  function run(q: string) {
    clearTimeout(timer);
    ctl?.abort();
    if (q.length < minChars) {
      options = [];
      busy = false;
      return;
    }
    busy = true;
    timer = setTimeout(async () => {
      const c = (ctl = new AbortController());
      try {
        const r = await search(q, c.signal);
        if (c.signal.aborted) return;
        options = r;
        active = r.length ? 0 : -1;
      } catch (e) {
        if (!c.signal.aborted) options = [];
      } finally {
        if (!c.signal.aborted) busy = false;
      }
    }, debounce);
  }

  function oninput(e: Event) {
    text = (e.currentTarget as HTMLInputElement).value;
    editing = true;
    open = true;
    run(text);
  }

  function openList() {
    if (disabled) return;
    if (!open) {
      open = true;
      run(editing ? text : '');
    }
  }

  function choose(o: ComboOption | null) {
    value = o;
    editing = false;
    text = '';
    open = false;
    active = -1;
    onselect?.(o);
  }

  function keydown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      if (!open) openList();
      else active = options.length ? (active + 1) % options.length : -1;
    } else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (open && options.length) active = (active - 1 + options.length) % options.length;
    } else if (e.key === 'Enter') {
      if (open && active >= 0 && options[active]) {
        e.preventDefault();
        choose(options[active]);
      }
    } else if (e.key === 'Escape') {
      if (open) {
        e.preventDefault();
        e.stopPropagation();
        open = false;
        editing = false;
      }
    } else if (e.key === 'Tab') {
      open = false;
      editing = false;
    }
  }

  function blur() {
    setTimeout(() => {
      if (document.activeElement !== input) {
        open = false;
        editing = false;
      }
    }, 120);
  }
</script>

<div class="h-combo">
  <input
    bind:this={input}
    class="h-input h-combo__input"
    type="text"
    role="combobox"
    autocomplete="off"
    aria-autocomplete="list"
    aria-expanded={open}
    aria-controls={listId}
    aria-activedescendant={open && active >= 0 ? `${uid}-o${active}` : undefined}
    aria-invalid={invalid || rest['aria-invalid'] || undefined}
    aria-describedby={rest['aria-describedby']}
    id={rest.id}
    {placeholder}
    {disabled}
    value={shown}
    {oninput}
    onfocus={openList}
    onclick={openList}
    onkeydown={keydown}
    onblur={blur}
  />
  {#if clearable && value && !disabled}
    <button type="button" class="h-combo__clear" aria-label={t('common.clear')} onclick={() => { choose(null); input?.focus(); }}>
      <Icon name="x" size={14} />
    </button>
  {/if}
  {#if open}
    <ul class="h-combo__list" id={listId} role="listbox">
      {#each options as o, i (o.value)}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <li
          id="{uid}-o{i}"
          role="option"
          class="h-combo__opt"
          class:h-combo__opt--active={i === active}
          aria-selected={value?.value === o.value}
          onpointerdown={(e) => e.preventDefault()}
          onclick={() => choose(o)}
        >
          <span>{o.label}</span>{#if o.hint}<span class="h-combo__hint">{o.hint}</span>{/if}
        </li>
      {:else}
        <li class="h-combo__note" role="presentation">{busy ? t('combobox.searching') : t('combobox.noOptions')}</li>
      {/each}
    </ul>
  {/if}
</div>
