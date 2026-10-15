<script lang="ts" generics="Row extends object">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  import Checkbox from './Checkbox.svelte';
  import Skeleton from './Skeleton.svelte';
  import { t } from '../i18n';

  import type { Column, Sort } from './table';

  let {
    columns,
    rows,
    rowKey,
    caption,
    sort,
    onsort,
    selectable = false,
    selected = $bindable(new Set<string>()),
    sticky = true,
    loading = false,
    empty,
    onrowclick,
  }: {
    columns: Column<Row>[];
    rows: Row[];
    rowKey: (row: Row) => string;
    /** Accessible name (visually hidden). */
    caption: string;
    sort?: Sort;
    onsort?: (key: string) => void;
    selectable?: boolean;
    selected?: Set<string>;
    sticky?: boolean;
    loading?: boolean;
    /** Shown instead of the body when there are no rows. */
    empty?: Snippet;
    onrowclick?: (row: Row) => void;
  } = $props();

  const all = $derived(rows.length > 0 && rows.every((r) => selected.has(rowKey(r))));
  const some = $derived(!all && rows.some((r) => selected.has(rowKey(r))));

  function toggleAll(on: boolean) {
    const next = new Set(selected);
    for (const r of rows) on ? next.add(rowKey(r)) : next.delete(rowKey(r));
    selected = next;
  }
  function toggle(k: string, on: boolean) {
    const next = new Set(selected);
    on ? next.add(k) : next.delete(k);
    selected = next;
  }
  const cell = (r: Row, c: Column<Row>) => String((r as Record<string, unknown>)[c.key] ?? '');
</script>

<div class="h-table" class:h-table--sticky={sticky}>
  <table class="h-table__table">
    <caption class="h-sr">{caption}</caption>
    <thead>
      <tr>
        {#if selectable}
          <th class="h-table__sel" scope="col">
            <Checkbox checked={all} indeterminate={some} aria-label={t('table.selectAll')} onchange={(e) => toggleAll(e.currentTarget.checked)} />
          </th>
        {/if}
        {#each columns as c (c.key)}
          <th
            scope="col"
            class:h-table__end={c.align === 'end'}
            style:width={c.width}
            aria-sort={sort?.key === c.key ? (sort.dir === 'asc' ? 'ascending' : 'descending') : c.sortable ? 'none' : undefined}
          >
            {#if c.sortable}
              <button type="button" class="h-table__sort" aria-label={t('table.sortBy', { column: c.label })} onclick={() => onsort?.(c.key)}>
                {c.label}
                {#if sort?.key === c.key}<Icon name={sort.dir === 'asc' ? 'arrow-up' : 'arrow-down'} size={14} />{/if}
              </button>
            {:else}{c.label}{/if}
          </th>
        {/each}
      </tr>
    </thead>
    <tbody>
      {#if loading && rows.length === 0}
        {#each Array(5) as _, i (i)}
          <tr aria-hidden="true">
            {#if selectable}<td></td>{/if}
            {#each columns as c (c.key)}<td><Skeleton /></td>{/each}
          </tr>
        {/each}
      {:else}
        {#each rows as r (rowKey(r))}
          {@const k = rowKey(r)}
          <tr class:h-table__row--sel={selected.has(k)} class:h-table__row--click={!!onrowclick} onclick={() => onrowclick?.(r)}>
            {#if selectable}
              <td class="h-table__sel" onclick={(e) => e.stopPropagation()}>
                <Checkbox checked={selected.has(k)} aria-label={t('table.selectRow')} onchange={(e) => toggle(k, e.currentTarget.checked)} />
              </td>
            {/if}
            {#each columns as c (c.key)}
              <td class:h-table__end={c.align === 'end'}>{#if c.cell}{@render c.cell(r)}{:else}{cell(r, c)}{/if}</td>
            {/each}
          </tr>
        {/each}
      {/if}
    </tbody>
  </table>
  {#if !loading && rows.length === 0}
    <div class="h-table__empty">{#if empty}{@render empty()}{:else}{t('table.empty')}{/if}</div>
  {/if}
</div>
