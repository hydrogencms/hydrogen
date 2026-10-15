<script lang="ts">
  import { localeState } from '../../lib/fields/locales';
  import { t } from '../../lib/i18n';

  let {
    locales,
    item,
  }: {
    locales: string[];
    item: { locales: string[]; draftParts: string[]; alwaysAvailable?: boolean; mainLocale?: string };
  } = $props();
</script>

<ul class="h-langs" aria-label={t('collections.completeness')}>
  {#each locales as l (l)}
    {@const s = localeState(l, item)}
    <li class="h-langs__pill">
      <span class="h-dot h-dot--{s === 'published' ? 'filled' : s === 'draft' ? 'half' : 'hollow'}" aria-hidden="true"></span>
      {l}
      <span class="h-sr">{t(`collections.locale.${s}`)}</span>
    </li>
  {/each}
</ul>

<style>
  .h-langs {
    display: flex;
    flex-wrap: wrap;
    gap: var(--h-space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .h-langs__pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    height: 22px;
    padding: 0 8px;
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius-pill);
    background: var(--h-surface-2);
    font-size: var(--h-text-xs);
    line-height: 1;
  }
</style>
