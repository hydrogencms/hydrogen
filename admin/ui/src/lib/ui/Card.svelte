<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';

  type Props = Omit<HTMLAttributes<HTMLElement>, 'children' | 'title'> & {
    title?: string;
    /** Padding: md 24 px, sm 20 px, none (tables, trees). */
    padding?: 'md' | 'sm' | 'none';
    /** Right side of the header row. */
    action?: Snippet;
    children: Snippet;
    /** Heading level of the title (default 2). */
    level?: 2 | 3;
  };

  let { title, padding = 'md', action, children, level = 2, class: cls = '', ...rest }: Props = $props();
  const uid = $props.id();
</script>

<section class="h-card h-card--{padding} {cls}" aria-labelledby={title ? `${uid}-t` : undefined} {...rest}>
  {#if title || action}
    <header class="h-card__head">
      {#if title}<svelte:element this={`h${level}`} class="h-card__title" id="{uid}-t">{title}</svelte:element>{/if}
      {#if action}<div class="h-card__action">{@render action()}</div>{/if}
    </header>
  {/if}
  {@render children()}
</section>
