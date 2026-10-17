<script lang="ts" module>
  export interface SegmentOption {
    value: string;
    label: string;
    icon?: string;
    /** A status dot before the label: filled = published, half = draft, hollow = missing. */
    dot?: 'filled' | 'half' | 'hollow';
    disabled?: boolean;
    /** A short muted note after the label, and a tooltip. */
    sub?: string;
    title?: string;
    /** Show only the icon; the label stays the accessible name. */
    iconOnly?: boolean;
  }
</script>

<script lang="ts">
  import Icon from './Icon.svelte';

  let {
    options,
    value = $bindable(''),
    label,
    size = 'md',
    onchange,
  }: {
    options: SegmentOption[];
    value?: string;
    /** Accessible name of the group. */
    label: string;
    size?: 'sm' | 'md';
    onchange?: (value: string) => void;
  } = $props();

  function pick(v: string) {
    if (v === value) return;
    value = v;
    onchange?.(v);
  }
</script>

<div class="h-seg h-seg--{size}" role="group" aria-label={label}>
  {#each options as o (o.value)}
    <button type="button" class="h-seg__btn" aria-pressed={o.value === value} aria-label={o.iconOnly ? o.label : undefined} title={o.iconOnly ? o.label : o.title} disabled={o.disabled} onclick={() => pick(o.value)}>
      {#if o.dot}<span class="h-dot h-dot--{o.dot}" aria-hidden="true"></span>{/if}
      {#if o.icon}<Icon name={o.icon} size={14} />{/if}
      {#if !o.iconOnly}{o.label}{/if}{#if o.sub}<span class="h-seg__sub">{o.sub}</span>{/if}
    </button>
  {/each}
</div>
