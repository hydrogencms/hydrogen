<script lang="ts">
  import { fromLocalInput } from '../../lib/fields/logic';
  import { t } from '../../lib/i18n';
  import type { CompiledType, CompiledField } from '../../lib/types.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import DatePicker from '../../lib/ui/DatePicker.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import { filterableFields, opsFor, type Filter, type ListQuery, type Op } from './query';

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
  const byName = (n: string) => fields.find((f) => f.name === n);
  let open = $state(false);
  let draft = $state<Filter[]>([]);
  const key = $derived(JSON.stringify(q.filters));
  $effect(() => {
    void key;
    draft = q.filters.map((f) => ({ ...f }));
    if (q.filters.length) open = true;
  });

  const elem = (f: CompiledField) => (f.shape.base === 'list' && f.of ? f.of : f);

  function addFilter() {
    const f = fields[0];
    if (!f) return;
    draft = [...draft, { field: f.name, op: 'eq', value: '' }];
    open = true;
  }
  function setField(i: number, name: string) {
    const f = byName(name);
    draft[i] = { field: name, op: f ? opsFor(f)[0]! : 'eq', value: '' };
  }
  function setOp(i: number, op: string) {
    draft[i] = { ...draft[i]!, op: op as Op, value: op === 'exists' ? 'true' : draft[i]!.value };
  }
  const apply = () => onchange({ filters: draft.filter((f) => f.field && (f.value !== '' || f.op === 'exists')) });
  const removeAt = (i: number) => {
    draft = draft.filter((_, n) => n !== i);
    apply();
  };
  const boolOptions = () => [
    { value: 'true', label: t('common.yes') },
    { value: 'false', label: t('common.no') },
  ];
  const dirty = $derived(JSON.stringify(draft.filter((f) => f.value !== '' || f.op === 'exists')) !== key);
</script>

<div class="h-filters" role="search" aria-label={t('collections.filters')}>
  <div class="h-filters__row">
    <Input
      type="search"
      class="h-filters__q"
      aria-label={t('collections.search')}
      placeholder={t('collections.searchPlaceholder')}
      value={search}
      oninput={(e) => onsearch((e.currentTarget as HTMLInputElement).value)}
    />
    <Select
      aria-label={t('collections.status.label')}
      value={q.status}
      onchange={(e) => onchange({ status: (e.currentTarget as HTMLSelectElement).value as ListQuery['status'] })}
      options={[
        { value: '', label: t('collections.status.any') },
        { value: 'published', label: t('collections.status.published') },
        { value: 'draft', label: t('collections.status.draft') },
      ]}
    />
    {#if offered.length > 1}
      <Select
        aria-label={t('collections.locale')}
        value={q.locale || offered[0]}
        onchange={(e) => onchange({ locale: (e.currentTarget as HTMLSelectElement).value })}
        options={offered.map((l) => ({ value: l, label: l }))}
      />
    {/if}
    {#if fields.length}
      <Button icon="filter" aria-expanded={open} onclick={() => (open = !open)}>
        {t('collections.filters')}{q.filters.length ? ` (${q.filters.length})` : ''}
      </Button>
    {/if}
  </div>

  {#if open && fields.length}
    <div class="h-filters__panel">
      {#each draft as f, i (i)}
        {@const def = byName(f.field)}
        <div class="h-filters__row" role="group" aria-label={t('collections.filterN', { n: i + 1 })}>
          <Select aria-label={t('collections.filterField')} value={f.field} onchange={(e) => setField(i, (e.currentTarget as HTMLSelectElement).value)} options={fields.map((x) => ({ value: x.name, label: x.label }))} />
          <Select aria-label={t('collections.filterOp')} value={f.op} onchange={(e) => setOp(i, (e.currentTarget as HTMLSelectElement).value)} options={(def ? opsFor(def) : []).map((o) => ({ value: o, label: t(`collections.op.${o}`) }))} />
          {#if f.op === 'exists' || (def && elem(def).shape.base === 'boolean' && f.op !== 'in')}
            <Select aria-label={t('collections.filterValue')} value={f.value} placeholder="" onchange={(e) => (draft[i]!.value = (e.currentTarget as HTMLSelectElement).value)} options={boolOptions()} />
          {:else if def && elem(def).shape.format === 'enum' && f.op !== 'in'}
            <Select aria-label={t('collections.filterValue')} value={f.value} placeholder="" onchange={(e) => (draft[i]!.value = (e.currentTarget as HTMLSelectElement).value)} options={(elem(def).shape.enum ?? []).map((v) => ({ value: v, label: v }))} />
          {:else if def && elem(def).shape.base === 'number' && f.op !== 'in'}
            <Input type="number" aria-label={t('collections.filterValue')} value={f.value} oninput={(e) => (draft[i]!.value = (e.currentTarget as HTMLInputElement).value)} onkeydown={(e) => e.key === 'Enter' && apply()} />
          {:else if def && elem(def).shape.format === 'date' && f.op !== 'in'}
            <DatePicker type="date" aria-label={t('collections.filterValue')} value={f.value} oninput={(e) => (draft[i]!.value = (e.currentTarget as HTMLInputElement).value)} />
          {:else if def && elem(def).shape.format === 'datetime' && f.op !== 'in'}
            <DatePicker type="datetime-local" aria-label={t('collections.filterValue')} value={f.value ? f.value.slice(0, 16) : ''} oninput={(e) => (draft[i]!.value = fromLocalInput((e.currentTarget as HTMLInputElement).value))} />
          {:else}
            <Input aria-label={t('collections.filterValue')} placeholder={f.op === 'in' ? t('collections.commaSeparated') : ''} value={f.value} oninput={(e) => (draft[i]!.value = (e.currentTarget as HTMLInputElement).value)} onkeydown={(e) => e.key === 'Enter' && apply()} />
          {/if}
          <IconButton icon="x" label={t('collections.removeFilter', { n: i + 1 })} onclick={() => removeAt(i)} />
        </div>
      {/each}
      <div class="h-filters__row">
        <Button size="sm" icon="plus" onclick={addFilter}>{t('collections.addFilter')}</Button>
        <Button size="sm" variant="primary" disabled={!dirty} onclick={apply}>{t('collections.apply')}</Button>
        {#if q.filters.length}<Button size="sm" variant="ghost" onclick={() => ((draft = []), apply())}>{t('collections.clearFilters')}</Button>{/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .h-filters {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-3);
  }
  .h-filters__row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--h-space-2);
  }
  .h-filters__row > :global(.h-select),
  .h-filters__row > :global(.h-input) {
    width: auto;
    min-width: 8rem;
  }
  .h-filters__row > :global(.h-filters__q) {
    flex: 1 1 14rem;
  }
  .h-filters__panel {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    padding: var(--h-space-3);
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius);
    background: var(--h-surface);
  }
</style>
