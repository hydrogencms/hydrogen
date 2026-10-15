<script lang="ts">
  import type { Snippet } from 'svelte';
  import Breadcrumbs from './Breadcrumbs.svelte';
  import Menu, { type MenuItem } from './Menu.svelte';
  import { t } from '../i18n';

  let {
    title,
    breadcrumbs,
    badge,
    meta,
    actions,
    overflow,
    primary,
  }: {
    title: string;
    /** Declares the top bar trail. */
    breadcrumbs?: Array<{ label: string; href?: string }>;
    /** Inline after the title (a status or type Badge). */
    badge?: Snippet;
    /** Muted line under the title: a string or a snippet. */
    meta?: string | Snippet;
    /** Secondary actions (light pills), left of the overflow menu. */
    actions?: Snippet;
    /** Rare and destructive actions in a ⋯ menu. */
    overflow?: MenuItem[];
    /** The single primary action (black pill), far right. */
    primary?: Snippet;
  } = $props();
</script>

{#if breadcrumbs}<Breadcrumbs items={breadcrumbs} />{/if}
<header class="h-pagehead">
  <div class="h-pagehead__main">
    <div class="h-pagehead__title">
      <h1>{title}</h1>
      {#if badge}{@render badge()}{/if}
    </div>
    {#if meta}<p class="h-pagehead__meta">{#if typeof meta === 'string'}{meta}{:else}{@render meta()}{/if}</p>{/if}
  </div>
  {#if actions || overflow?.length || primary}
    <div class="h-pagehead__actions">
      {@render actions?.()}
      {#if overflow?.length}<Menu label={t('common.more')} items={overflow} />{/if}
      {@render primary?.()}
    </div>
  {/if}
</header>
