<script lang="ts">
  import type { Snippet } from 'svelte';

  export interface TabItem {
    id: string;
    label: string;
    count?: number;
    disabled?: boolean;
  }
  let {
    tabs,
    value = $bindable(),
    label,
    children,
  }: {
    tabs: TabItem[];
    value?: string;
    label: string;
    /** Panel content for the active tab id. */
    children: Snippet<[string]>;
  } = $props();

  const uid = $props.id();
  const active = $derived(tabs.some((t) => t.id === value) ? value! : tabs[0]?.id);

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
    value = tabs[to].id;
    document.getElementById(`${uid}-t-${tabs[to].id}`)?.focus();
  }
</script>

<div class="h-tabs">
  <div class="h-tabs__list" role="tablist" aria-label={label}>
    {#each tabs as tab, i (tab.id)}
      <button
        type="button"
        role="tab"
        id="{uid}-t-{tab.id}"
        class="h-tabs__tab"
        aria-selected={tab.id === active}
        aria-controls="{uid}-p-{tab.id}"
        tabindex={tab.id === active ? 0 : -1}
        disabled={tab.disabled}
        onclick={() => (value = tab.id)}
        onkeydown={(e) => key(e, i)}
      >
        {tab.label}{#if tab.count !== undefined}<span class="h-tabs__count">{tab.count}</span>{/if}
      </button>
    {/each}
  </div>
  {#if active}
    <div class="h-tabs__panel" role="tabpanel" id="{uid}-p-{active}" aria-labelledby="{uid}-t-{active}" tabindex="0">
      {@render children(active)}
    </div>
  {/if}
</div>
