<script lang="ts">
  import type { HTMLInputAttributes } from 'svelte/elements';
  import Icon from './Icon.svelte';

  type Props = Omit<HTMLInputAttributes, 'value'> & {
    value?: string | number | null;
    invalid?: boolean;
    el?: HTMLInputElement | null;
    /** A leading icon (`search` for search boxes). */
    icon?: string;
    /** 44 px tall (login). */
    large?: boolean;
  };

  let { value = $bindable(''), invalid = false, el = $bindable(null), type = 'text', icon, large = false, class: cls = '', ...rest }: Props = $props();
</script>

{#if icon}
  <span class="h-inputwrap {cls}">
    <Icon name={icon} size={16} />
    <input bind:this={el} class="h-input h-input--icon" class:h-input--lg={large} {type} bind:value aria-invalid={invalid || undefined} {...rest} />
  </span>
{:else}
  <input bind:this={el} class="h-input {cls}" class:h-input--lg={large} {type} bind:value aria-invalid={invalid || undefined} {...rest} />
{/if}
