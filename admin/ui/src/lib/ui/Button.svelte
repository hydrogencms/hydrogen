<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLButtonAttributes } from 'svelte/elements';
  import Icon from './Icon.svelte';

  type Props = Omit<HTMLButtonAttributes, 'children'> & {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
    size?: 'sm' | 'md';
    icon?: string;
    loading?: boolean;
    /** Render an <a> instead of a <button>. */
    href?: string;
    children?: Snippet;
  };

  let {
    variant = 'secondary',
    size = 'md',
    icon,
    loading = false,
    href,
    type = 'button',
    disabled,
    children,
    ...rest
  }: Props = $props();
  const cls = $derived(`h-button h-button--${variant} h-button--${size}`);
</script>

{#if href && !disabled}
  <a class={cls} {href} {...rest as object}>
    {#if icon}<Icon name={icon} size={16} />{/if}{@render children?.()}
  </a>
{:else}
  <button class={cls} {type} disabled={disabled || loading} aria-busy={loading || undefined} {...rest}>
    {#if loading}<span class="h-spinner" aria-hidden="true"></span>{:else if icon}<Icon name={icon} size={16} />{/if}
    {@render children?.()}
  </button>
{/if}
