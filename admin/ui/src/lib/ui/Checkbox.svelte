<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLInputAttributes } from 'svelte/elements';

  type Props = Omit<HTMLInputAttributes, 'checked' | 'type' | 'children'> & {
    checked?: boolean;
    indeterminate?: boolean;
    /** Visible label; or pass children. For a label-less box set aria-label. */
    label?: string;
    children?: Snippet;
  };

  let { checked = $bindable(false), indeterminate = false, label, children, class: cls = '', ...rest }: Props = $props();
</script>

<label class="h-check {cls}">
  <input type="checkbox" class="h-check__box" bind:checked {indeterminate} {...rest} />
  {#if label || children}<span class="h-check__label">{#if children}{@render children()}{:else}{label}{/if}</span>{/if}
</label>
