<script lang="ts">
  import type { HTMLSelectAttributes } from 'svelte/elements';

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
  };

  let { value = $bindable(''), options, placeholder, invalid = false, class: cls = '', ...rest }: Props = $props();
</script>

<select class="h-input h-select {cls}" bind:value aria-invalid={invalid || undefined} {...rest}>
  {#if placeholder !== undefined}<option value="">{placeholder}</option>{/if}
  {#each options as o (o.value)}
    <option value={o.value} disabled={o.disabled}>{o.label}</option>
  {/each}
</select>
