<script lang="ts">
  import type { HTMLSelectAttributes } from 'svelte/elements';
  import Icon from './Icon.svelte';

  export interface SelectOption {
    value: string;
    label: string;
    disabled?: boolean;
  }
  type Props = Omit<HTMLSelectAttributes, 'value' | 'children'> & {
    value?: string;
    options: SelectOption[];
    /** Adds a first empty option with this label. */
    placeholder?: string;
    invalid?: boolean;
    /** A small pill select (32 px) for toolbars and table footers. */
    compact?: boolean;
  };

  let { value = $bindable(''), options, placeholder, invalid = false, compact = false, class: cls = '', ...rest }: Props = $props();
</script>

<span class="h-selectwrap {cls}" class:h-selectwrap--sm={compact}>
  <select class="h-input h-select" bind:value aria-invalid={invalid || undefined} {...rest}>
    {#if placeholder !== undefined}<option value="">{placeholder}</option>{/if}
    {#each options as o (o.value)}
      <option value={o.value} disabled={o.disabled}>{o.label}</option>
    {/each}
  </select>
  <Icon name="chevron-down" size={16} />
</span>
