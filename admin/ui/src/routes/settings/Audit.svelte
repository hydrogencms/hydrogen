<script lang="ts">
  import '../../lib/messages/settings';
  import { api, type Schemas } from '../../lib/api/client';
  import { formatDateTime } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import DatePicker from '../../lib/ui/DatePicker.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import { auditQuery, emptyFilters, filtersFromQuery, type AuditFilters } from './audit';
  import { reportError } from '../../lib/errors';

  type Entry = Schemas['AuditEntry'];
  const applied = $derived(filtersFromQuery(router.query));
  let draft = $state<AuditFilters>(emptyFilters());
  let items = $state<Entry[] | null>(null);
  let hasMore = $state(false);
  let failed = $state(false);
  let loadingMore = $state(false);
  let pages = 0;
  /** Cursor of the next page (`next` of the last response, sent as `before`). */
  let next = '';
  let seq = 0;

  async function load(f: AuditFilters, more: boolean) {
    const mine = ++seq;
    failed = false;
    if (more) loadingMore = true;
    else items = null;
    try {
      const q = auditQuery(f, more && !next ? pages : 0);
      const r = (await api.get('/audit', { query: (more && next ? { ...q, before: next } : q) as never })) as { items: Entry[]; hasMore: boolean; next?: string | number };
      if (mine !== seq) return;
      items = more ? [...(items ?? []), ...r.items] : r.items;
      pages = more ? pages + 1 : 1;
      hasMore = r.hasMore;
      next = r.hasMore && r.next !== undefined ? String(r.next) : '';
    } catch (e) {
      if (mine !== seq) return;
      failed = true;
      reportError(e);
    } finally {
      loadingMore = false;
    }
  }
  $effect(() => {
    draft = { ...applied };
    void load(applied, false);
  });

  function apply() {
    router.setQuery({
      actor: draft.actor.trim(),
      action: draft.action.trim(),
      subject: draft.subject.trim(),
      since: draft.since,
    });
  }
  function clear() {
    router.setQuery({ actor: undefined, action: undefined, subject: undefined, since: undefined });
  }

  const who = (e: Entry) => (e.actorId ? `${e.actorKind}:${e.actorId}` : e.actorKind);
  type Data = { actor?: { model?: string; task?: string } } & Record<string, unknown>;
  const agent = (e: Entry) => (e.data as Data | undefined)?.actor;
  const json = (v: unknown) => JSON.stringify(v ?? null, null, 2);
</script>

<div class="h-page">
  <div class="h-page__head"><h1>{t('nav.audit')}</h1></div>
  <form
    class="h-audit__filters"
    role="search"
    aria-label={t('settings.audit.filters')}
    onsubmit={(e) => {
      e.preventDefault();
      apply();
    }}>
    <Field label={t('settings.audit.actor')} description={t('settings.audit.actorHint')}>
      {#snippet children(c)}<Input {...c} bind:value={draft.actor} placeholder="user:… / agent" />{/snippet}
    </Field>
    <Field label={t('settings.audit.action')} description={t('settings.audit.actionHint')}>
      {#snippet children(c)}<Input {...c} bind:value={draft.action} placeholder="content.publish, token.*" />{/snippet}
    </Field>
    <Field label={t('settings.audit.subject')}>
      {#snippet children(c)}<Input {...c} bind:value={draft.subject} />{/snippet}
    </Field>
    <Field label={t('settings.audit.since')}>
      {#snippet children(c)}<DatePicker {...c} type="datetime-local" bind:value={draft.since} />{/snippet}
    </Field>
    <div class="h-row">
      <Button type="submit" variant="primary" icon="filter">{t('settings.audit.apply')}</Button>
      <Button onclick={clear}>{t('common.clear')}</Button>
    </div>
  </form>

  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={() => load(applied, false)}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t('nav.audit')}
      loading={!items}
      rows={items ?? []}
      rowKey={(r) => String(r.id)}
      columns={[
        { key: 'at', label: t('settings.audit.when'), cell: atCell },
        { key: 'actor', label: t('settings.audit.actor'), cell: actorCell },
        { key: 'action', label: t('settings.audit.action'), cell: actionCell },
        { key: 'subject', label: t('settings.audit.subject'), cell: subjectCell },
        { key: 'data', label: t('settings.audit.data'), cell: dataCell },
      ]}>
      {#snippet empty()}<p class="h-muted">{t('settings.audit.empty')}</p>{/snippet}
    </Table>
    {#if hasMore}
      <div class="h-row">
        <Button loading={loadingMore} onclick={() => load(applied, true)}>{t('settings.audit.more')}</Button>
      </div>
    {/if}
  {/if}
</div>

{#snippet atCell(r: Entry)}{formatDateTime(r.at)}{/snippet}
{#snippet actorCell(r: Entry)}
  {who(r)}
  {#if agent(r)}<br /><span class="h-muted">{[agent(r)?.model, agent(r)?.task].filter(Boolean).join(' · ')}</span>{/if}
{/snippet}
{#snippet actionCell(r: Entry)}<Badge>{r.action}</Badge>{/snippet}
{#snippet subjectCell(r: Entry)}{r.subject ?? ''}{/snippet}
{#snippet dataCell(r: Entry)}
  {#if r.data !== undefined && r.data !== null}
    <details>
      <summary>{t('settings.audit.show')}</summary>
      <pre class="h-audit__json">{json(r.data)}</pre>
    </details>
  {/if}
{/snippet}

<style>
  .h-audit__filters {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
    gap: var(--h-space-3);
    align-items: end;
    margin-bottom: var(--h-space-4);
  }
  .h-audit__json {
    max-width: 36rem;
    max-height: 18rem;
    overflow: auto;
    margin: var(--h-space-2) 0 0;
    font-size: var(--h-text-xs);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
