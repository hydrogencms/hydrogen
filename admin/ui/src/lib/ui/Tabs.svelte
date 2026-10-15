<script lang="ts" module>
  export interface TabItem {
    id: string;
    label: string;
    count?: number;
    disabled?: boolean;
    icon?: string;
  }
</script>

<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';

  let {
    tabs,
    value = $bindable(),
    label,
    variant = 'underline',
    children,
    trailing,
    onchange,
  }: {
    tabs: TabItem[];
    value?: string;
    label: string;
    /** `underline`: page-level view tabs; `segmented`: a control inside a panel. Labels never wrap. */
    variant?: 'underline' | 'segmented';
    /** Panel content for the active tab id. Omit for tabs that only drive the page (navigation, saved views). */
    children?: Snippet<[string]>;
    /** Extra content at the end of the tab row (a "+ Save view" ghost tab). */
    trailing?: Snippet;
    onchange?: (id: string) => void;
  } = $props();

  const uid = $props.id();
  const active = $derived(tabs.some((t) => t.id === value) ? value! : tabs[0]?.id);

  function select(id: string) {
    value = id;
    onchange?.(id);
  }
  function key(e: KeyboardEvent, i: number) {
    const ok = tabs.map((t, n) => (t.disabled ? -1 : n)).filter((n) => n >= 0);
    const at = ok.indexOf(i);
    let to: number | undefined;
    if (e.key === 'ArrowRight') to = ok[(at + 1) % ok.length];
    else if (e.key === 'ArrowLeft') to = ok[(at - 1 + ok.length) % ok.length];
    else if (e.key === 'Home') to = ok[0];
    else if (e.key === 'End') to = ok[ok.length - 1];
    if (to === undefined) return;
    e.preventDefault();
    select(tabs[to].id);
    document.getElementById(`${uid}-t-${tabs[to].id}`)?.focus();
  }
</script>

<div class="h-tabs h-tabs--{variant}">
  <div class="h-tabs__bar">
    <div class="h-tabs__list" role="tablist" aria-label={label}>
      {#each tabs as tab, i (tab.id)}
        <button
          type="button"
          role="tab"
          id="{uid}-t-{tab.id}"
          class="h-tabs__tab"
          aria-selected={tab.id === active}
          aria-controls={children ? `${uid}-p-${tab.id}` : undefined}
          tabindex={tab.id === active ? 0 : -1}
          disabled={tab.disabled}
          onclick={() => select(tab.id)}
          onkeydown={(e) => key(e, i)}
        >
          {#if tab.icon}<Icon name={tab.icon} size={14} />{/if}{tab.label}{#if tab.count !== undefined}<span class="h-tabs__count h-num">{tab.count}</span>{/if}
        </button>
      {/each}
    </div>
    {#if trailing}{@render trailing()}{/if}
  </div>
  {#if children && active}
    <div class="h-tabs__panel" role="tabpanel" id="{uid}-p-{active}" aria-labelledby="{uid}-t-{active}" tabindex="0">
      {@render children(active)}
    </div>
  {/if}
</div>
