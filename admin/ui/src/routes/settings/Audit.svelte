<script lang="ts">
  import Slot from '../../lib/ext/Slot.svelte';
  import '../../lib/messages/settings';
  import '../../lib/messages/history';
  import { api, type Schemas } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import Avatar from '../../lib/ui/Avatar.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import DatePicker from '../../lib/ui/DatePicker.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Popover from '../../lib/ui/Popover.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import {
    ACTION_GROUPS, SINCE_PRESETS, assignmentRole, auditQuery, describeEntry, filtersFromQuery, humanizeAction, itemIdOf, referencedIds, releaseOf, restoredVersion, reviewBypassed, shortId, subjectKind, type AuditFilters,
  } from './audit';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import { formatDateTime } from '../../lib/format';
  import SettingsHead from './SettingsHead.svelte';
  import { reportError } from '../../lib/errors';

  type Entry = Schemas['AuditEntry'];
  const applied = $derived(filtersFromQuery(router.query));
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
      void resolve(r.items);
    } catch (e) {
      if (mine !== seq) return;
      failed = true;
      reportError(e);
    } finally {
      loadingMore = false;
    }
  }
  $effect(() => {
    void load(applied, false);
  });

  // Names for ids, keyed "kind:id". Directories are listed once; item names are fetched per id (draft view).
  let names = $state<Record<string, string>>({});
  const dirs = new Map<string, Promise<void>>();
  const dir = (kind: string, fetcher: () => Promise<[string, string][]>) => {
    if (!dirs.has(kind)) {
      dirs.set(
        kind,
        fetcher()
          .then((list) => {
            for (const [id, n] of list) names[`${kind}:${id}`] = n;
          })
          .catch(() => {}),
      );
    }
    return dirs.get(kind)!;
  };
  const directory: Record<string, () => Promise<[string, string][]>> = {
    user: async () => (await api.get('/users', { query: { limit: 500 } })).items.map((u) => [u.id, u.name || u.login]),
    group: async () => (await api.get('/groups', { query: { limit: 500 } })).items.map((g) => [g.id, g.name]),
    role: async () => (await api.get('/roles', { query: { limit: 500 } })).items.map((r) => [r.id, r.name]),
    token: async () => (await api.get('/tokens', { query: { limit: 500 } })).items.map((k) => [k.id, k.name]),
  };

  async function resolve(batch: Entry[]) {
    const kinds = new Set<string>();
    const ids = new Set<string>();
    for (const e of batch) {
      if (e.actorKind === 'user') kinds.add('user');
      if (e.actorKind === 'token' || e.actorKind === 'agent') kinds.add('token');
      for (const [kind, id] of referencedIds(e)) {
        if (kind === 'item') ids.add(id);
        else if (kind in directory) kinds.add(kind);
      }
    }
    const wait: Promise<unknown>[] = [...kinds].map((k) => dir(k, directory[k]!));
    for (const id of [...ids].filter((i) => !(`item:${i}` in names)).slice(0, 40)) {
      names[`item:${id}`] = '';
      wait.push(
        api
          .get('/items/{id}', { params: { id }, query: { draft: true } })
          .then((it) => (names[`item:${id}`] = it.name))
          .catch(() => {}),
      );
    }
    await Promise.all(wait);
  }

  type Actor = { name: string; sub?: string; icon?: string };
  type Data = { actor?: { model?: string; task?: string } } & Record<string, unknown>;
  function actorOf(e: Entry): Actor {
    const id = e.actorId ?? '';
    const agent = (e.data as Data | undefined)?.actor;
    const sub = agent ? [agent.model, agent.task].filter(Boolean).join(' · ') || undefined : undefined;
    switch (e.actorKind) {
      case 'user':
        return { name: id && id === session.principal?.id ? t('dashboard.you') : (names[`user:${id}`] ?? t('dashboard.someone')) };
      case 'token':
        return { name: id === 'config' ? t('dashboard.configToken') : (names[`token:${id}`] ?? t('dashboard.apiToken')), icon: 'key', sub };
      case 'agent':
        return { name: names[`token:${id}`] ?? t('dashboard.agent'), icon: 'cpu', sub };
      case 'system':
        return { name: t('dashboard.system'), icon: 'settings' };
      default:
        return { name: t('dashboard.someone') };
    }
  }

  const verbKey = (a: string) => `settings.audit.v.${a}`;
  const verb = (a: string) => (t(verbKey(a)) !== verbKey(a) ? t(verbKey(a)) : humanizeAction(a));
  type Subject = { text: string; to?: string; tail?: { word?: string; text: string; to?: string } };
  function subjectOf(e: Entry): Subject | undefined {
    const k = subjectKind(e.action);
    const d = e.data as { principalKind?: string; principalId?: string; user?: string } | undefined;
    if (k === 'assignment') {
      const r = assignmentRole(e);
      const role = r.name ?? (r.id ? names[`role:${r.id}`] : undefined);
      if (!role) return undefined;
      const who = d?.principalKind && d.principalId ? names[`${d.principalKind}:${d.principalId}`] : undefined;
      const path = { user: 'users', group: 'groups', token: 'tokens' }[d?.principalKind ?? ''];
      return {
        text: role,
        tail: who ? { word: e.action === 'assignment.delete' ? t('settings.audit.from') : t('settings.audit.to'), text: who, to: d?.principalKind === 'token' ? '/settings/tokens' : path ? `/settings/${path}/${d?.principalId}` : undefined } : undefined,
      };
    }
    if (e.action === 'auth.login' || e.action === 'auth.logout' || e.action === 'auth.login_failed') return undefined;
    const rel = releaseOf(e);
    if (rel) return { text: rel.name || shortId(rel.id), to: `/releases/${rel.id}` };
    const itemId = itemIdOf(e);
    if (itemId) {
      const version = restoredVersion(e);
      return {
        text: names[`item:${itemId}`] || shortId(itemId),
        to: `/items/${itemId}`,
        tail: version !== undefined ? { word: '·', text: t('settings.audit.version', { n: version }) } : undefined,
      };
    }
    // A page entry that records no item is about a tree node, which has no readable name here.
    if (k === 'page' || !e.subject) return undefined;
    const text = names[`${k === 'auth' ? 'user' : k}:${e.subject}`] || shortId(e.subject);
    const to = { user: `/settings/users/${e.subject}`, group: `/settings/groups/${e.subject}`, role: `/settings/roles/${e.subject}`, token: '/settings/tokens' }[k];
    const member = e.action.startsWith('group.member_') && d?.user ? names[`user:${d.user}`] || (e.data as { login?: string }).login : undefined;
    return { text, to, tail: member ? { text: member, to: `/settings/users/${d!.user}` } : undefined };
  }
  const json = (v: unknown) => JSON.stringify(v ?? null, null, 2);

  // The details dialog: the entry is described from the names already loaded, plus the few ids only it mentions.
  let sel = $state<Entry | null>(null);
  let detailsOpen = $state(false);
  const lookup = (kind: string, id: string) => names[`${kind}:${id}`] || undefined;
  const desc = $derived(sel ? describeEntry(sel, lookup, t) : null);
  function show(e: Entry) {
    sel = e;
    detailsOpen = true;
    void resolve([e]);
  }

  // Filters. Each popover writes the URL straight away; the free-text ones apply on Enter or the button.
  const set = (patch: Record<string, string | undefined>) => router.setQuery(patch);
  const anyFilter = $derived(!!(applied.actor || applied.action || applied.subject || applied.since));
  const actorKinds = ['user', 'token', 'agent', 'system'];
  const actorLabel = (a: string) => (actorKinds.includes(a) ? t(`settings.audit.kind.${a}`) : a);
  const actionLabel = (a: string) => {
    const g = ACTION_GROUPS.find((x) => x.pattern === a);
    return g ? t(`settings.audit.group.${g.id}`) : a;
  };
  const sinceLabel = (s: string) => (s in SINCE_PRESETS ? t(`settings.audit.since.${s}`) : s.replace('T', ' '));
  let actorText = $state('');
  let actionText = $state('');
  let subjectText = $state('');
  let sinceText = $state('');
  $effect(() => {
    actorText = applied.actor;
    actionText = applied.action;
    subjectText = applied.subject;
    sinceText = applied.since in SINCE_PRESETS ? '' : applied.since;
  });
</script>

<div class="h-page">
  <SettingsHead section="audit" />
  <div class="h-aud__bar" role="group" aria-label={t('settings.audit.filters')}>
    <Popover label={t('settings.audit.actor')} triggerClass="h-aud__f {applied.actor ? 'h-aud__f--on' : ''}">
      {#snippet trigger()}<Icon name="user" size={14} />{applied.actor ? `${t('settings.audit.actor')}: ${actorLabel(applied.actor)}` : t('settings.audit.actor')}<Icon name="chevron-down" size={14} />{/snippet}
      {#snippet children(close)}
        <div class="h-aud__opts">
          {#each actorKinds as k (k)}
            <button type="button" class="h-aud__opt" aria-pressed={applied.actor === k} onclick={() => { set({ actor: applied.actor === k ? undefined : k }); close(); }}>{t(`settings.audit.kind.${k}`)}{#if applied.actor === k}<Icon name="check" size={14} />{/if}</button>
          {/each}
          <form class="h-aud__custom" onsubmit={(e) => { e.preventDefault(); set({ actor: actorText.trim() || undefined }); close(); }}>
            <Input aria-label={t('settings.audit.actorHint')} placeholder="user:… / agent" bind:value={actorText} />
            <Button type="submit" size="sm">{t('settings.audit.apply')}</Button>
          </form>
        </div>
      {/snippet}
    </Popover>
    <Popover label={t('settings.audit.action')} triggerClass="h-aud__f {applied.action ? 'h-aud__f--on' : ''}">
      {#snippet trigger()}<Icon name="list" size={14} />{applied.action ? `${t('settings.audit.action')}: ${actionLabel(applied.action)}` : t('settings.audit.action')}<Icon name="chevron-down" size={14} />{/snippet}
      {#snippet children(close)}
        <div class="h-aud__opts">
          {#each ACTION_GROUPS as g (g.id)}
            <button type="button" class="h-aud__opt" aria-pressed={applied.action === g.pattern} onclick={() => { set({ action: applied.action === g.pattern ? undefined : g.pattern }); close(); }}>{t(`settings.audit.group.${g.id}`)}{#if applied.action === g.pattern}<Icon name="check" size={14} />{/if}</button>
          {/each}
          <form class="h-aud__custom" onsubmit={(e) => { e.preventDefault(); set({ action: actionText.trim() || undefined }); close(); }}>
            <Input aria-label={t('settings.audit.actionHint')} placeholder="item.publish, token.*" bind:value={actionText} />
            <Button type="submit" size="sm">{t('settings.audit.apply')}</Button>
          </form>
        </div>
      {/snippet}
    </Popover>
    <Popover label={t('settings.audit.since')} triggerClass="h-aud__f {applied.since ? 'h-aud__f--on' : ''}">
      {#snippet trigger()}<Icon name="clock" size={14} />{applied.since ? `${t('settings.audit.since')}: ${sinceLabel(applied.since)}` : t('settings.audit.since')}<Icon name="chevron-down" size={14} />{/snippet}
      {#snippet children(close)}
        <div class="h-aud__opts">
          {#each Object.keys(SINCE_PRESETS) as k (k)}
            <button type="button" class="h-aud__opt" aria-pressed={applied.since === k} onclick={() => { set({ since: applied.since === k ? undefined : k }); close(); }}>{t(`settings.audit.since.${k}`)}{#if applied.since === k}<Icon name="check" size={14} />{/if}</button>
          {/each}
          <form class="h-aud__custom" onsubmit={(e) => { e.preventDefault(); set({ since: sinceText || undefined }); close(); }}>
            <DatePicker type="datetime-local" aria-label={t('settings.audit.sinceCustom')} bind:value={sinceText} />
            <Button type="submit" size="sm">{t('settings.audit.apply')}</Button>
          </form>
        </div>
      {/snippet}
    </Popover>
    <Popover label={t('settings.audit.subject')} triggerClass="h-aud__f {applied.subject ? 'h-aud__f--on' : ''}">
      {#snippet trigger()}<Icon name="hash" size={14} />{applied.subject ? `${t('settings.audit.subject')}: ${shortId(applied.subject)}` : t('settings.audit.subject')}<Icon name="chevron-down" size={14} />{/snippet}
      {#snippet children(close)}
        <form class="h-aud__custom" onsubmit={(e) => { e.preventDefault(); set({ subject: subjectText.trim() || undefined }); close(); }}>
          <Input aria-label={t('settings.audit.subject')} placeholder={t('settings.audit.subjectHint')} bind:value={subjectText} />
          <Button type="submit" size="sm">{t('settings.audit.apply')}</Button>
        </form>
      {/snippet}
    </Popover>
    {#if anyFilter}
      <Button variant="ghost" size="sm" onclick={() => set({ actor: undefined, action: undefined, subject: undefined, since: undefined })}>{t('settings.audit.clearAll')}</Button>
    {/if}
  </div>

  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={() => load(applied, false)}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t('nav.audit')}
      loading={!items}
      rows={items ?? []}
      rowKey={(r) => String(r.id)}
      columns={[
        { key: 'at', label: t('settings.audit.when'), cell: atCell, width: '9rem' },
        { key: 'actor', label: t('settings.audit.actor'), cell: actorCell },
        { key: 'event', label: t('settings.audit.event'), cell: eventCell },
        { key: 'data', label: t('settings.audit.data'), cell: dataCell, mobile: 'hide' },
      ]}>
      {#snippet empty()}
        <EmptyState icon="history" title={t('settings.audit.empty')} description={anyFilter ? t('settings.audit.emptyFiltered') : undefined}>
          {#snippet actions()}{#if anyFilter}<Button onclick={() => set({ actor: undefined, action: undefined, subject: undefined, since: undefined })}>{t('settings.audit.clearAll')}</Button>{/if}{/snippet}
        </EmptyState>
      {/snippet}
      {#snippet footer()}
        {#if hasMore}
          <Button size="sm" loading={loadingMore} onclick={() => load(applied, true)}>{t('settings.audit.more')}</Button>
        {:else if items && items.length > 0}
          <span class="h-muted">{t('settings.audit.end')}</span>
        {/if}
      {/snippet}
    </Table>
  {/if}
  <Slot at="settings.after" props={{section: 'audit'}} />
</div>

{#snippet atCell(r: Entry)}<Time at={r.at} />{/snippet}
{#snippet actorCell(r: Entry)}
  {@const a = actorOf(r)}
  <span class="h-aud__actor">
    <Avatar name={a.name} size={28} icon={a.icon} />
    <span class="h-aud__who">
      <span>{a.name}</span>
      {#if a.sub}<span class="h-muted h-aud__sub">{a.sub}</span>{/if}
    </span>
  </span>
{/snippet}
{#snippet eventCell(r: Entry)}
  {@const s = subjectOf(r)}
  <span title={r.action}>
    {verb(r.action)}{#if r.action === 'auth.login_failed' && !r.subject}<span class="h-muted"> ({t('settings.audit.unknownLogin')})</span>{/if}
    {#if s}{#if s.to}<a use:link href={href(s.to)}>{s.text}</a>{:else}<strong>{s.text}</strong>{/if}{#if s.tail}
        {s.tail.word ?? '·'} {#if s.tail.to}<a use:link href={href(s.tail.to)}>{s.tail.text}</a>{:else}<strong>{s.tail.text}</strong>{/if}{/if}{/if}
    {#if reviewBypassed(r)}<span class="h-muted"> ({t('settings.audit.bypassed')})</span>{/if}
  </span>
{/snippet}
{#snippet dataCell(r: Entry)}
  <Button variant="ghost" size="sm" onclick={() => show(r)}>{t('settings.audit.details')}</Button>
{/snippet}

<Dialog bind:open={detailsOpen} title={t('settings.audit.detailsOf')} size="lg" onclose={() => (sel = null)}>
  {#if sel && desc}
    {@const a = actorOf(sel)}
    {@const s = subjectOf(sel)}
    <p class="h-aud__lead">{desc.text}</p>
    <dl class="h-aud__dl">
      <dt>{t('settings.audit.when')}</dt>
      <dd>{formatDateTime(sel.at)}</dd>
      <dt>{t('settings.audit.actor')}</dt>
      <dd>{a.name}{#if a.sub}<span class="h-muted"> · {a.sub}</span>{/if}</dd>
      <dt>{t('settings.audit.action')}</dt>
      <dd>{verb(sel.action)} <code class="h-muted">{sel.action}</code></dd>
      {#if s}
        <dt>{t('settings.audit.subject')}</dt>
        <dd>{#if s.to}<a use:link href={href(s.to)} onclick={() => (detailsOpen = false)}>{s.text}</a>{:else}<strong>{s.text}</strong>{/if}{#if s.tail}
            {s.tail.word ?? '·'} {s.tail.text}{/if}</dd>
      {/if}
    </dl>
    <h3 class="h-aud__h">{t('settings.audit.changes')}</h3>
    {#if desc.changes.length}
      <dl class="h-aud__dl">
        {#each desc.changes as c, i (i)}
          <dt>{c.label}</dt>
          <dd>{c.value}</dd>
        {/each}
      </dl>
    {:else}
      <p class="h-muted">{t('settings.audit.noChanges')}</p>
    {/if}
    {#if sel.data !== undefined && sel.data !== null}
      <details class="h-aud__tech">
        <summary class="h-aud__sum"><span class="h-aud__chev"><Icon name="chevron-right" size={14} /></span>{t('settings.audit.technical')}</summary>
        <pre class="h-audit__json">{json(sel.data)}</pre>
      </details>
    {/if}
  {/if}
</Dialog>

<style>
  .h-aud__sum {
    display: inline-flex;
    align-items: center;
    gap: var(--h-space-1);
    list-style: none;
    cursor: pointer;
  }
  .h-aud__sum::-webkit-details-marker {
    display: none;
  }
  .h-aud__chev {
    display: inline-grid;
    transition: rotate var(--h-dur-fast);
  }
  :global(details[open]) .h-aud__chev {
    rotate: 90deg;
  }
  .h-aud__bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--h-space-2);
  }
  .h-aud__bar :global(.h-aud__f) {
    display: inline-flex;
    align-items: center;
    gap: var(--h-space-1);
    height: var(--h-control-height-sm);
    padding: 0 var(--h-space-3);
    border: 1px solid var(--h-border-strong);
    border-radius: var(--h-radius-pill);
    background: var(--h-surface);
    color: var(--h-text);
    font: inherit;
    font-size: var(--h-text-sm);
    font-weight: 500;
    cursor: pointer;
  }
  .h-aud__bar :global(.h-aud__f:hover) {
    background: var(--h-surface-2);
  }
  .h-aud__bar :global(.h-aud__f--on),
  .h-aud__bar :global(.h-aud__f--on:hover) {
    border-color: var(--h-accent);
    background: var(--h-accent);
    color: var(--h-accent-contrast);
  }
  :global(.h-aud__opts) {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 14rem;
  }
  :global(.h-aud__opt) {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--h-space-3);
    padding: var(--h-space-2) var(--h-space-3);
    border: 0;
    border-radius: var(--h-radius);
    background: transparent;
    color: var(--h-text);
    font: inherit;
    font-size: var(--h-text-md);
    text-align: left;
    cursor: pointer;
  }
  :global(.h-aud__opt:hover) {
    background: var(--h-surface-2);
  }
  :global(.h-aud__opt[aria-pressed='true']) {
    font-weight: 600;
  }
  :global(.h-aud__custom) {
    display: flex;
    gap: var(--h-space-2);
    align-items: center;
    padding-top: var(--h-space-2);
    margin-top: var(--h-space-1);
    border-top: 1px solid var(--h-border);
  }
  .h-aud__actor {
    display: flex;
    align-items: center;
    gap: var(--h-space-2);
    min-width: 0;
  }
  .h-aud__who {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .h-aud__sub {
    font-size: var(--h-text-xs);
  }
  .h-aud__lead {
    margin: 0 0 var(--h-space-4);
    font-size: var(--h-text-lg, 1.05rem);
    font-weight: 600;
  }
  .h-aud__h {
    margin: var(--h-space-4) 0 var(--h-space-2);
    font-size: var(--h-text-md);
  }
  .h-aud__dl {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: var(--h-space-1) var(--h-space-4);
    margin: 0;
  }
  .h-aud__dl dt {
    color: var(--h-text-muted);
  }
  .h-aud__dl dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .h-aud__tech {
    margin-top: var(--h-space-4);
  }
  .h-audit__json {
    max-height: 18rem;
    overflow: auto;
    margin: var(--h-space-2) 0 0;
    font-size: var(--h-text-xs);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
