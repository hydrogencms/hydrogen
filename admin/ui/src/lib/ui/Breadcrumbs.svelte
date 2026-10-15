<script lang="ts">
  import { link, href } from '../router.svelte';
  import { crumbs } from '../crumbs.svelte';
  import { t } from '../i18n';
  import Icon from './Icon.svelte';

  let {
    items,
    label = t('nav.breadcrumb'),
    inline = false,
  }: {
    items: Array<{ label: string; href?: string }>;
    label?: string;
    /** Render the trail here. By default it only declares the trail; the top bar shows it. */
    inline?: boolean;
  } = $props();

  $effect(() => {
    if (inline) return;
    return crumbs.set(items);
  });
</script>

{#if inline}
  <nav class="h-crumbs" aria-label={label}>
    <ol class="h-crumbs__list">
      {#each items as it, i (i)}
        <li class="h-crumbs__item">
          {#if i > 0}<Icon name="chevron-right" size={14} />{/if}
          {#if it.href && i < items.length - 1}
            <a use:link href={href(it.href)}>{it.label}</a>
          {:else}
            <span aria-current={i === items.length - 1 ? 'page' : undefined}>{it.label}</span>
          {/if}
        </li>
      {/each}
    </ol>
  </nav>
{/if}
