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

<ul class="h-dots" aria-label={t('collections.completeness')}>
  {#each locales as l (l)}
    {@const s = localeState(l, item)}
    <li class="h-dots__item">
      <span class="h-dots__dot h-dots__dot--{s}" aria-hidden="true"></span>
      <span class="h-dots__code">{l}</span>
      <span class="h-sr">{t(`collections.locale.${s}`)}</span>
    </li>
  {/each}
</ul>

<style>
  .h-dots {
    display: flex;
    flex-wrap: wrap;
    gap: var(--h-space-1) var(--h-space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .h-dots__item {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: var(--h-text-xs);
  }
  .h-dots__dot {
    box-sizing: border-box;
    width: 0.6rem;
    height: 0.6rem;
    border: 2px solid var(--h-border-strong);
    border-radius: 50%;
  }
  .h-dots__dot--published {
    border-color: var(--h-success);
    background: var(--h-success);
  }
  .h-dots__dot--draft {
    border-color: var(--h-warning);
    background: linear-gradient(90deg, var(--h-warning) 50%, transparent 50%);
  }
  .h-dots__dot--fallback {
    border-color: var(--h-info);
    border-style: dotted;
  }
</style>
