<script lang="ts">
  import { fromLocalInput } from '../../lib/fields/logic';
  import { t } from '../../lib/i18n';
  import type { CompiledField } from '../../lib/types.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import DatePicker from '../../lib/ui/DatePicker.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import { opsFor, type Filter, type Op } from './query';

  let {
    fields,
    filter,
    onapply,
  }: {
    fields: CompiledField[];
    /** The filter to edit; absent for a new one. */
    filter?: Filter;
    onapply: (f: Filter) => void;
  } = $props();

  const initial = () => filter ?? { field: fields[0]?.name ?? '', op: 'eq' as Op, value: '' };
  let draft = $state<Filter>({ ...initial() });

  const def = $derived(fields.find((f) => f.name === draft.field));
  const elem = $derived(def && def.shape.base === 'list' && def.of ? def.of : def);
  const single = $derived(draft.op !== 'in');
  const ready = $derived(draft.field !== '' && (draft.value !== '' || draft.op === 'exists'));

  function setField(name: string) {
    const f = fields.find((x) => x.name === name);
    draft = { field: name, op: f ? opsFor(f)[0]! : 'eq', value: '' };
  }
  function setOp(op: string) {
    draft = { ...draft, op: op as Op, value: op === 'exists' ? 'true' : draft.op === 'exists' ? '' : draft.value };
  }
  const submit = () => ready && onapply({ ...draft });
  const yesNo = () => [
    { value: 'true', label: t('common.yes') },
    { value: 'false', label: t('common.no') },
  ];
</script>

<form
  class="h-fed"
  onsubmit={(e) => {
    e.preventDefault();
    submit();
  }}
>
  <Select aria-label={t('collections.filterField')} value={draft.field} onchange={(e) => setField((e.currentTarget as HTMLSelectElement).value)} options={fields.map((x) => ({ value: x.name, label: x.label }))} />
  <Select aria-label={t('collections.filterOp')} value={draft.op} onchange={(e) => setOp((e.currentTarget as HTMLSelectElement).value)} options={(def ? opsFor(def) : []).map((o) => ({ value: o, label: t(`collections.op.${o}`) }))} />
  {#if draft.op === 'exists' || (elem && elem.shape.base === 'boolean' && single)}
    <Select aria-label={t('collections.filterValue')} value={draft.value} placeholder="" onchange={(e) => (draft.value = (e.currentTarget as HTMLSelectElement).value)} options={yesNo()} />
  {:else if elem && elem.shape.format === 'enum' && single}
    <Select aria-label={t('collections.filterValue')} value={draft.value} placeholder="" onchange={(e) => (draft.value = (e.currentTarget as HTMLSelectElement).value)} options={(elem.shape.enum ?? []).map((v) => ({ value: v, label: v }))} />
  {:else if elem && elem.shape.base === 'number' && single}
    <Input type="number" aria-label={t('collections.filterValue')} value={draft.value} oninput={(e) => (draft.value = (e.currentTarget as HTMLInputElement).value)} />
  {:else if elem && elem.shape.format === 'date' && single}
    <DatePicker type="date" aria-label={t('collections.filterValue')} value={draft.value} oninput={(e) => (draft.value = (e.currentTarget as HTMLInputElement).value)} />
  {:else if elem && elem.shape.format === 'datetime' && single}
    <DatePicker type="datetime-local" aria-label={t('collections.filterValue')} value={draft.value ? draft.value.slice(0, 16) : ''} oninput={(e) => (draft.value = fromLocalInput((e.currentTarget as HTMLInputElement).value))} />
  {:else}
    <Input aria-label={t('collections.filterValue')} placeholder={draft.op === 'in' ? t('collections.commaSeparated') : ''} value={draft.value} oninput={(e) => (draft.value = (e.currentTarget as HTMLInputElement).value)} />
  {/if}
  <Button type="submit" variant="primary" size="sm" disabled={!ready}>{t('collections.apply')}</Button>
</form>

<style>
  .h-fed {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    min-width: 15rem;
  }
</style>
