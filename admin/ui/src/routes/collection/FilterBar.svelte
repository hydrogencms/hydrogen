<script lang="ts">
  import { t } from '../../lib/i18n';
  import type { CompiledType } from '../../lib/types.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Popover from '../../lib/ui/Popover.svelte';
  import Segmented from '../../lib/ui/Segmented.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import FilterEditor from './FilterEditor.svelte';
  import { filterableFields, type Filter, type ListQuery } from './query';

  let {
    type,
    q,
    offered,
    search,
    onsearch,
    onchange,
  }: {
    type: CompiledType;
    q: ListQuery;
    offered: string[];
    search: string;
    onsearch: (v: string) => void;
    onchange: (patch: Partial<ListQuery>) => void;
  } = $props();

  const fields = $derived(filterableFields(type.fields));

  function chipText(f: Filter): string {
    const def = fields.find((x) => x.name === f.field);
    const name = def?.label ?? f.field;
    const elem = def && def.shape.base === 'list' && def.of ? def.of : def;
    if (f.op === 'exists') return `${name}: ${f.value === 'false' ? t('collections.op.notExists') : t('collections.op.exists')}`;
    const v = elem?.shape.base === 'boolean' && f.op !== 'in' ? (f.value === 'true' ? t('common.yes') : t('common.no')) : f.value;
    return `${name}: ${f.op === 'eq' ? '' : t(`collections.op.${f.op}`) + ' '}${v}`;
  }
  const setAt = (i: number, f: Filter) => onchange({ filters: q.filters.map((x, n) => (n === i ? f : x)) });
  const removeAt = (i: number) => onchange({ filters: q.filters.filter((_, n) => n !== i) });
</script>

<div class="h-tb" role="search" aria-label={t('collections.filters')}>
  <Input
    type="search"
    icon="search"
    class="h-tb__q"
    aria-label={t('collections.search')}
    placeholder={t('collections.searchPlaceholder')}
    value={search}
    oninput={(e) => onsearch((e.currentTarget as HTMLInputElement).value)}
  />
  <div class="h-tb__chips">
    {#each q.filters as f, i (i + JSON.stringify(f))}
      <span class="h-chip h-chip--applied">
        <Popover label={t('collections.editFilter', { name: chipText(f) })} triggerClass="h-chip__main">
          {#snippet trigger()}{chipText(f)}<Icon name="chevron-down" size={14} />{/snippet}
          {#snippet children(close)}
            <FilterEditor {fields} filter={f} onapply={(nf) => (close(), setAt(i, nf))} />
          {/snippet}
        </Popover>
        <button class="h-chip__x" type="button" aria-label={t('collections.removeFilter', { n: i + 1 })} onclick={() => removeAt(i)}><Icon name="x" size={14} /></button>
      </span>
    {/each}
    {#if fields.length}
      <span class="h-chip h-chip--ghost">
        <Popover label={t('collections.addFilter')} triggerClass="h-chip__main">
          {#snippet trigger()}<Icon name="plus" size={14} />{t('collections.addFilter')}{/snippet}
          {#snippet children(close)}
            <FilterEditor {fields} onapply={(nf) => (close(), onchange({ filters: [...q.filters, nf] }))} />
          {/snippet}
        </Popover>
      </span>
    {/if}
    {#if q.filters.length}
      <Button variant="ghost" size="sm" onclick={() => onchange({ filters: [] })}>{t('collections.clearAll')}</Button>
    {/if}
  </div>
  <div class="h-tb__right">
    {#if offered.length > 1}
      <Select
        class="h-tb__locale"
        aria-label={t('collections.locale')}
        value={q.locale || offered[0]}
        onchange={(e) => onchange({ locale: (e.currentTarget as HTMLSelectElement).value })}
        options={offered.map((l) => ({ value: l, label: l }))}
      />
    {/if}
    <Segmented
      label={t('collections.status.label')}
      value={q.status}
      onchange={(v) => onchange({ status: v as ListQuery['status'] })}
      options={[
        { value: '', label: t('collections.status.any') },
        { value: 'draft', label: t('collections.status.draft') },
        { value: 'published', label: t('collections.status.published') },
      ]}
    />
  </div>
</div>

<style>
  .h-tb {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--h-space-2) var(--h-space-3);
  }
  .h-tb > :global(.h-tb__q) {
    flex: 0 1 320px;
    width: 320px;
    max-width: 100%;
  }
  .h-tb__chips {
    display: flex;
    flex: 1 1 0;
    align-items: center;
    gap: var(--h-space-2);
    min-width: 0;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .h-tb__chips > :global(*) {
    flex: none;
  }
  .h-tb__right {
    display: flex;
    align-items: center;
    gap: var(--h-space-2);
    margin-inline-start: auto;
  }
  .h-tb__right :global(.h-tb__locale) {
    width: 6.5rem;
  }
  @media (max-width: 767px) {
    .h-tb > :global(.h-tb__q),
    .h-tb__chips {
      flex: 1 1 100%;
      width: 100%;
    }
    .h-tb__right {
      margin-inline-start: 0;
      flex-wrap: wrap;
    }
  }
</style>
