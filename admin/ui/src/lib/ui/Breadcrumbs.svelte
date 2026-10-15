<script lang="ts">
  import { link } from '../router.svelte';
  import { href } from '../router.svelte';
  import { t } from '../i18n';

  let { items, label = t('nav.main') }: { items: Array<{ label: string; href?: string }>; label?: string } = $props();
</script>

<nav class="h-crumbs" aria-label={label}>
  <ol class="h-crumbs__list">
    {#each items as it, i (i)}
      <li class="h-crumbs__item">
        {#if it.href && i < items.length - 1}
          <a use:link href={href(it.href)}>{it.label}</a>
        {:else}
          <span aria-current={i === items.length - 1 ? 'page' : undefined}>{it.label}</span>
        {/if}
      </li>
    {/each}
  </ol>
</nav>
