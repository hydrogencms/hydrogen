<script lang="ts">
  import '../messages/history';
  import { t } from '../i18n';
  import { href, link } from '../router.svelte';
  import Badge from '../ui/Badge.svelte';
  import Self from './DiffView.svelte';
  import type { BlockRow, Change, FieldDiff, Ref } from './diff';
  import type { Seg } from './seq';

  // Heading level of the field names: the page puts these under an h2.
  let { change, depth = 3 }: { change: Change; depth?: number } = $props();
  const h = (d: number) => `h${Math.min(Math.max(d, 1), 6)}`;

  const tone = (s: string) => (s === 'added' ? 'success' : s === 'removed' ? 'danger' : s === 'moved' ? 'info' : 'warning');
  // The status is a word and a sign, never only a colour.
  const sign = (s: string) => (s === 'added' ? '+' : s === 'removed' ? '−' : s === 'moved' ? '↕' : '~');

  function show(v: unknown): string {
    if (v === undefined || v === null || v === '') return '';
    if (typeof v === 'boolean') return v ? t('common.yes') : t('common.no');
    if (typeof v === 'object') return JSON.stringify(v, null, 2);
    return String(v);
  }
</script>

{#snippet segs(list: Seg[])}
  <span class="h-diff__segs"
    >{#each list as s, i (i)}{#if s.k === 'ins'}<ins class="h-diff__ins"><span class="h-sr">{t('diff.ins')} </span>{s.text}<span class="h-sr"> {t('diff.insEnd')}</span></ins>{:else if s.k === 'del'}<del class="h-diff__del"><span class="h-sr">{t('diff.del')} </span>{s.text}<span class="h-sr"> {t('diff.delEnd')}</span></del>{:else}{s.text}{/if}{/each}</span
  >
{/snippet}

{#snippet badge(status: string)}
  <Badge tone={tone(status)}><span aria-hidden="true">{sign(status)}</span> {t(`diff.${status}`)}</Badge>
{/snippet}

{#snippet ref(r: Ref | undefined, kind: 'ins' | 'del')}
  {#if !r}
    <span class="h-muted">{t('diff.empty')}</span>
  {:else}
    {@const label = r.name || r.id}
    {#if kind === 'ins'}
      <ins class="h-diff__ins"><span class="h-sr">{t('diff.ins')} </span><a use:link href={href(`/items/${r.id}`)}>{label}</a></ins>
    {:else}
      <del class="h-diff__del"><span class="h-sr">{t('diff.del')} </span><a use:link href={href(`/items/${r.id}`)}>{label}</a></del>
    {/if}
  {/if}
{/snippet}

{#snippet note(approx?: boolean)}
  {#if approx}<p class="h-muted h-diff__note">{t('diff.approx')}</p>{/if}
{/snippet}

{#snippet fieldRow(f: FieldDiff, d: number)}
  <section class="h-diff__field" aria-label={f.label}>
    <header class="h-diff__head">
      <svelte:element this={h(d)} class="h-diff__label">{f.label}</svelte:element>
      {@render badge(f.status)}
    </header>
    <div class="h-diff__body"><Self change={f.change} depth={d + 1} /></div>
  </section>
{/snippet}

{#snippet blockRows(rows: BlockRow[], same: number, d: number)}
  <ol class="h-diff__blocks">
    {#each rows as r, i (i)}
      <li class="h-diff__block h-diff__block--{r.status}">
        <div class="h-diff__blockhead">
          {#if r.status === 'context'}
            <Badge>{r.label}</Badge>
          {:else}
            {@render badge(r.status)}
            <strong>{r.label}</strong>
          {/if}
          {#if r.summary}<span class="h-muted h-diff__sum">{r.summary}</span>{/if}
          {#if r.moved && r.status !== 'moved'}<Badge tone="info"><span aria-hidden="true">↕</span> {t('diff.movedBlock')}</Badge>{/if}
          {#if r.nested}<span class="h-muted">{t('diff.nested', { n: r.nested })}</span>{/if}
        </div>
        {#each r.props as p (p.name)}
          {@render fieldRow(p, d)}
        {/each}
        {#if r.status === 'changed' && r.sameProps.length}
          <p class="h-muted h-diff__note">{t('diff.sameProps', { names: r.sameProps.join(', ') })}</p>
        {/if}
        {#each r.slots as s (s.name)}
          <div class="h-diff__slot">
            <svelte:element this={h(d)} class="h-diff__slotname">{t('diff.slot', { slot: s.label })}</svelte:element>
            {@render blockRows(s.rows, s.same, d + 1)}
          </div>
        {/each}
      </li>
    {/each}
  </ol>
  {#if same > 0}<p class="h-muted h-diff__note">{t('diff.blockSame', { n: same })}</p>{/if}
{/snippet}

{#if change.kind === 'text'}
  <p class="h-diff__text">{@render segs(change.segs)}</p>
  {@render note(change.approx)}
{:else if change.kind === 'value'}
  {@const before = show(change.before)}
  {@const after = show(change.after)}
  <dl class="h-diff__pair">
    <dt>{t('diff.before')}</dt>
    <dd>{#if before}<del class="h-diff__del"><span class="h-sr">{t('diff.del')} </span><span class="h-diff__val">{before}</span></del>{:else}<span class="h-muted">{t('diff.empty')}</span>{/if}</dd>
    <dt>{t('diff.after')}</dt>
    <dd>{#if after}<ins class="h-diff__ins"><span class="h-sr">{t('diff.ins')} </span><span class="h-diff__val">{after}</span></ins>{:else}<span class="h-muted">{t('diff.empty')}</span>{/if}</dd>
  </dl>
{:else if change.kind === 'ref'}
  <dl class="h-diff__pair">
    <dt>{t('diff.before')}</dt>
    <dd>{@render ref(change.before, 'del')}</dd>
    <dt>{t('diff.after')}</dt>
    <dd>{@render ref(change.after, 'ins')}</dd>
  </dl>
{:else if change.kind === 'rich'}
  <ol class="h-diff__rows">
    {#each change.rows as r, i (i)}
      <li class="h-diff__rrow h-diff__rrow--{r.status}">
        <div class="h-diff__rhead">
          {@render badge(r.status)}
          <span class="h-muted">{r.node}{#if r.from} ({t('diff.nodeChanged', { from: r.from })}){/if}</span>
          {#if r.formatting}<span class="h-muted">· {t('diff.formatting')}</span>{/if}
        </div>
        <p class="h-diff__text">{@render segs(r.segs)}</p>
        {@render note(r.approx)}
      </li>
    {/each}
  </ol>
  {#if change.same > 0}<p class="h-muted h-diff__note">{t('diff.rowsSame', { n: change.same })}</p>{/if}
{:else if change.kind === 'list'}
  <ol class="h-diff__rows">
    {#each change.rows as r (r.status + r.index)}
      <li class="h-diff__rrow h-diff__rrow--{r.status}">
        <div class="h-diff__rhead">
          {@render badge(r.status)}
          <span class="h-muted">{t('diff.item', { n: r.index })}</span>
        </div>
        <Self change={r.change} {depth} />
      </li>
    {/each}
  </ol>
  {#if change.same > 0}<p class="h-muted h-diff__note">{t('diff.rowsSame', { n: change.same })}</p>{/if}
{:else if change.kind === 'group'}
  {#each change.fields as f (f.name)}
    {@render fieldRow(f, depth)}
  {/each}
  {#if change.same.length}<p class="h-muted h-diff__note">{t('diff.sameProps', { names: change.same.join(', ') })}</p>{/if}
{:else if change.kind === 'blocks'}
  {@render blockRows(change.rows, change.same, depth)}
{/if}

<style>
  .h-diff__field {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    min-width: 0;
  }
  .h-diff__head,
  .h-diff__rhead,
  .h-diff__blockhead {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--h-space-2);
  }
  .h-diff__label {
    margin: 0;
    font-size: var(--h-text-sm);
    font-weight: 600;
  }
  .h-diff__body {
    min-width: 0;
  }
  .h-diff__text {
    margin: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    line-height: 1.6;
  }
  .h-diff__ins,
  .h-diff__del {
    border-radius: 3px;
    padding: 0 2px;
    text-decoration-thickness: 2px;
  }
  .h-diff__ins {
    background: var(--h-success-soft);
    color: var(--h-success);
    text-decoration: underline;
  }
  .h-diff__del {
    background: var(--h-danger-soft);
    color: var(--h-danger);
    text-decoration: line-through;
  }
  .h-diff__ins :global(a),
  .h-diff__del :global(a) {
    color: inherit;
    text-decoration: inherit;
  }
  .h-diff__val {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .h-diff__pair {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--h-space-1) var(--h-space-3);
    margin: 0;
    font-size: var(--h-text-sm);
  }
  .h-diff__pair dt {
    color: var(--h-text-muted);
  }
  .h-diff__pair dd {
    margin: 0;
    min-width: 0;
  }
  .h-diff__rows,
  .h-diff__blocks {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .h-diff__rrow,
  .h-diff__block {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    padding-left: var(--h-space-3);
    border-left: 3px solid var(--h-border-strong);
    min-width: 0;
  }
  .h-diff__rrow--added,
  .h-diff__block--added {
    border-left-style: solid;
    border-left-color: var(--h-success);
  }
  .h-diff__rrow--removed,
  .h-diff__block--removed {
    border-left-style: dashed;
    border-left-color: var(--h-danger);
  }
  .h-diff__rrow--changed,
  .h-diff__block--changed,
  .h-diff__block--moved {
    border-left-style: double;
    border-left-width: 4px;
    border-left-color: var(--h-warning);
  }
  .h-diff__sum {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 24rem;
  }
  .h-diff__note {
    margin: 0;
    font-size: var(--h-text-xs);
  }
  .h-diff__slot {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    margin-left: var(--h-space-3);
  }
  .h-diff__slotname {
    margin: 0;
    font-size: var(--h-text-xs);
    font-weight: 600;
    color: var(--h-text-muted);
  }
</style>
