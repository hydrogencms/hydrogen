<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLInputAttributes } from 'svelte/elements';

  type Props = Omit<HTMLInputAttributes, 'checked' | 'type' | 'children'> & {
    checked?: boolean;
    label?: string;
    children?: Snippet;
  };

  let { checked = $bindable(false), label, children, class: cls = '', ...rest }: Props = $props();
</script>

<label class="h-switch {cls}">
  <input type="checkbox" role="switch" class="h-switch__input" bind:checked {...rest} />
  <span class="h-switch__track" aria-hidden="true"></span>
  {#if label || children}<span class="h-switch__label">{#if children}{@render children()}{:else}{label}{/if}</span>{/if}
</label>
