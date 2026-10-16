<script lang="ts">
  import { api, type Schemas } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { channels } from '../../lib/channels.svelte';
  import { types } from '../../lib/types.svelte';
  import { href, link } from '../../lib/router.svelte';
  import { dayPart } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import Card from '../../lib/ui/Card.svelte';
  import Avatar from '../../lib/ui/Avatar.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import { ext } from '../../lib/extensions.svelte';
  import Extension from '../../lib/ext/Extension.svelte';
  import Slot from '../../lib/ext/Slot.svelte';

  type Audit = Schemas['AuditEntry'];
  interface Row {
    key: number;
    who: string;
    icon?: string;
    verb: string;
    deleted?: boolean;
    trashed?: boolean;
    id?: string;
    name?: string;
    type?: string;
    at: string;
  }
  interface Rec {
    e: Audit;
    who: string;
    icon?: string;
    actions: string[];
  }

  const first = $derived((session.principal?.name ?? '').trim().split(/\s+/)[0] ?? '');
  const greeting = $derived(t('dashboard.welcome', { greeting: t(`dashboard.greeting.${dayPart()}`), name: first }));
  const today = new Intl.DateTimeFormat(undefined, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' }).format(new Date());

  const readable = $derived(types.listable.filter((x) => channels.showsType(x.name) && session.can('content.read', { type: x.name })));
  const creatable = $derived(readable.filter((x) => session.can('content.create', { type: x.name })));
  const locales = $derived.by(() => {
    const src = channels.active ? [channels.active] : channels.items.filter((c) => session.can('content.read', { channel: c.name }));
    return new Set(src.flatMap((c) => c.languages ?? [])).size;
  });
  const canAudit = $derived(session.can('audit.read'));

  const WEEK = 7 * 86_400_000;
  let drafts = $state<number | null | undefined>(null); // null: loading, undefined: not available
  let published = $state<{ n: number; more: boolean } | null | undefined>(null);
  let rows = $state<Row[] | null>(null);
  let myDrafts = $state<Row[] | null>(null);
  let failed = $state(false);

  // One count request per readable collection for the drafts stat; unreadable ones simply do not count.
  $effect(() => {
    if (!types.loaded) return;
    const names = readable.map((x) => x.name);
    drafts = null;
    if (names.length === 0) {
      drafts = undefined;
      return;
    }
    let live = true;
    void Promise.allSettled(names.map((type) => api.get('/collections/{type}/items', { params: { type }, query: { status: 'draft', limit: 1 } }))).then((rs) => {
      if (!live) return;
      const ok = rs.filter((r) => r.status === 'fulfilled');
      drafts = ok.length ? ok.reduce((n, r) => n + r.value.total, 0) : undefined;
    });
    return () => (live = false);
  });

  $effect(() => {
    if (!canAudit) {
      published = undefined;
      return;
    }
    let live = true;
    const since = new Date(Date.now() - WEEK).toISOString();
    api
      .get('/audit', { query: { action: 'item.publish', since, limit: 100 } })
      .then((r) => live && (published = { n: new Set(r.items.map((e) => e.subject)).size, more: r.hasMore }))
      .catch(() => live && (published = undefined));
    return () => (live = false);
  });

  const verbs = ['item.create', 'item.update', 'item.publish', 'item.unpublish', 'item.trash', 'item.restore', 'item.purge', 'item.delete', 'item.discard'];
  const GONE = ['item.purge', 'item.delete'];
  const TEN_MIN = 600_000;

  // Directory lookups are cached for the session: users and tokens are listed once.
  let users: Promise<Map<string, string>> | undefined;
  let tokens: Promise<Map<string, string>> | undefined;
  session.onClear(() => {
    users = tokens = undefined;
  });
  const loadUsers = () => (users ??= api.get('/users', { query: { limit: 200 } }).then((r) => new Map(r.items.map((u) => [u.id, u.name || u.login]))).catch(() => new Map<string, string>()));
  const loadTokens = () => (tokens ??= api.get('/tokens', { query: { limit: 200 } }).then((r) => new Map(r.items.map((x) => [x.id, x.name]))).catch(() => new Map<string, string>()));

  async function actorOf(e: Audit): Promise<{ who: string; icon?: string }> {
    const id = e.actorId ?? '';
    switch (e.actorKind) {
      case 'user':
        return { who: id && id === session.principal?.id ? t('dashboard.you') : ((await loadUsers()).get(id) ?? t('dashboard.someone')) };
      case 'token':
        return { who: id === 'config' ? t('dashboard.configToken') : ((await loadTokens()).get(id) ?? t('dashboard.apiToken')), icon: 'key' };
      case 'agent':
        return { who: (await loadTokens()).get(id) ?? t('dashboard.agent'), icon: 'cpu' };
      case 'system':
        return { who: t('dashboard.system'), icon: 'settings' };
      default:
        return { who: t('dashboard.someone') };
    }
  }

  function verbText(actions: string[]): string {
    const set = new Set(actions);
    if (set.has('item.create')) set.delete('item.update');
    const list = verbs.filter((v) => set.has(v)).map((v) => t(`dashboard.verb.${v}`));
    return new Intl.ListFormat(undefined, { type: 'conjunction' }).format(list);
  }

  async function loadRecent() {
    failed = false;
    try {
      const entries = (await api.get('/audit', { query: { action: 'item.*', limit: 50 } })).items.filter((e) => verbs.includes(e.action));
      // Newest first; fold events of the same actor on the same item within ten minutes into one sentence.
      const recs: Rec[] = [];
      for (const e of entries) {
        const hit = recs.find((r) => r.e.actorKind === e.actorKind && r.e.actorId === e.actorId && r.e.subject === e.subject && Math.abs(Date.parse(r.e.at) - Date.parse(e.at)) <= TEN_MIN);
        if (hit) hit.actions.push(e.action);
        else recs.push({ e, who: '', actions: [e.action] });
      }
      const feed = recs.slice(0, 8);
      // The newest action on each item says where it is now: purged items are not asked for, trashed ones from the trash.
      const last = new Map<string, string>();
      for (const e of entries) if (e.subject && !last.has(e.subject)) last.set(e.subject, e.action);
      const items = await Promise.all(
        [...new Set(feed.map((r) => r.e.subject).filter((x): x is string => !!x && !GONE.includes(last.get(x) ?? '')))].map((id) =>
          api
            .get('/items/{id}', { params: { id }, query: (last.get(id) === 'item.trash' ? { draft: true, trashed: true } : { draft: true }) as never })
            .then((it) => [id, { name: it.name, type: it.type }] as const)
            .catch(() => null),
        ),
      ).then((r) => new Map(r.filter((x) => x !== null)));
      const actors = await Promise.all(feed.map((r) => actorOf(r.e)));
      rows = feed.map((r, i) => {
        const it = r.e.subject ? items.get(r.e.subject) : undefined;
        return { key: r.e.id, ...actors[i]!, verb: verbText(r.actions), deleted: GONE.includes(last.get(r.e.subject ?? '') ?? ''), trashed: last.get(r.e.subject ?? '') === 'item.trash', id: it ? r.e.subject : undefined, name: it?.name, type: it?.type, at: r.e.at };
      });
    } catch {
      failed = true;
    }
  }
  $effect(() => {
    if (canAudit) void loadRecent();
  });

  // Without audit.read: the principal's newest drafts from the lists.
  $effect(() => {
    if (canAudit || !types.loaded) return;
    let live = true;
    void Promise.allSettled(
      readable.slice(0, 8).map((ty) =>
        api.get('/collections/{type}/items', { params: { type: ty.name }, query: { status: 'draft', sort: '-updatedAt', limit: 5 } }),
      ),
    ).then((rs) => {
      if (!live) return;
      myDrafts = rs
        .flatMap((r) => (r.status === 'fulfilled' ? r.value.items : []))
        .sort((a, b) => b.updatedAt.localeCompare(a.updatedAt))
        .slice(0, 6)
        .map((i, n) => ({ key: n, who: '', verb: '', id: i.id, name: i.name, type: i.type, at: i.updatedAt }));
    });
    return () => (live = false);
  });

  const typeLabel = (n?: string) => (n ? (types.get(n)?.label ?? n) : '');
  const newHref = (type: string) => `/collections/${type}/new${channels.languages?.[0] ? `?locale=${channels.languages[0]}` : ''}`;
</script>

{#snippet stat(label: string, icon: string, value: number | string | null | undefined, note: string)}
  {#if value !== undefined}
    <Card class="h-stat h-lg-span-3 h-md-span-6">
      <p class="h-stat__label"><Icon name={icon} size={16} />{label}</p>
      {#if value === null}<Skeleton height="2.5rem" width="4rem" />{:else}<p class="h-stat__figure">{value}</p>{/if}
      <p class="h-stat__note">{note}</p>
    </Card>
  {/if}
{/snippet}

{#snippet sentence(r: Row)}
  <strong>{r.who}</strong> {r.verb}
  {#if r.id && !r.deleted}<a use:link href={href(r.trashed ? `/trash/${r.id}` : `/items/${r.id}`)}>{r.name || t('dashboard.anItem')}</a>{:else}<strong>{r.name || t('dashboard.anItem')}</strong>{/if}
{/snippet}

<div class="h-page">
  <Slot at="dashboard.before" />
  <div class="h-pagehead">
    <div class="h-pagehead__main">
      <h1>{greeting}</h1>
      <p class="h-pagehead__meta">{today}</p>
    </div>
  </div>

  <div class="h-page-grid">
    {@render stat(t('dashboard.stat.drafts'), 'edit', drafts, t('dashboard.stat.draftsNote'))}
    {@render stat(t('dashboard.stat.published'), 'send', published === null || published === undefined ? published : published.more ? `${published.n}+` : published.n, t('dashboard.stat.publishedNote'))}
    {@render stat(t('dashboard.stat.collections'), 'layers', types.loaded ? readable.length : null, t('dashboard.stat.collectionsNote'))}
    {@render stat(t('dashboard.stat.locales'), 'globe', channels.loaded ? locales : null, t('dashboard.stat.localesNote'))}

    {#if canAudit}
      <Card class="h-lg-span-8" title={t('dashboard.recent')}>
        {#snippet action()}<a use:link href={href('/settings/audit')}>{t('dashboard.viewAll')}</a>{/snippet}
        {#if failed}
          <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={loadRecent}>{t('app.retry')}</Button></p>
        {:else if !rows}
          <Skeleton lines={5} height="2.25rem" />
        {:else if rows.length === 0}
          <p class="h-muted">{t('dashboard.recentEmpty')}</p>
        {:else}
          <ul class="h-feed">
            {#each rows as r (r.key)}
              <li class="h-feed__item">
                <Avatar name={r.who} size={32} icon={r.icon} />
                <div class="h-feed__text">
                  <span>{@render sentence(r)}</span>
                  <span class="h-feed__sub">{#if r.type}{typeLabel(r.type) + ' · '}{/if}<Time at={r.at} /></span>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      </Card>
    {:else}
      <Card class="h-lg-span-8" title={t('dashboard.drafts')}>
        {#if !myDrafts}
          <Skeleton lines={4} height="2.25rem" />
        {:else if myDrafts.length === 0}
          <p class="h-muted">{t('dashboard.draftsEmpty')}</p>
        {:else}
          <ul class="h-feed">
            {#each myDrafts as r (r.key)}
              <li class="h-feed__item">
                <div class="h-feed__text">
                  <a use:link href={href(`/items/${r.id}`)}>{r.name || t('dashboard.anItem')}</a>
                  <span class="h-feed__sub">{typeLabel(r.type) + ' · '}<Time at={r.at} /></span>
                </div>
              </li>
            {/each}
          </ul>
        {/if}
      </Card>
    {/if}

    <Card class="h-lg-span-4" title={t('dashboard.quick')}>
      {#if !types.loaded}
        <Skeleton lines={4} height="2.25rem" />
      {:else if creatable.length === 0}
        <p class="h-muted">{t('dashboard.quickEmpty')}</p>
      {:else}
        <ul class="h-quick">
          {#each creatable as ty (ty.name)}
            <li class="h-quick__item">
              <span class="h-tile"><Icon name={ty.admin?.icon ?? 'file'} size={16} /></span>
              <span class="h-quick__name">{ty.label}</span>
              <a use:link class="h-button h-button--secondary h-button--sm" href={href(newHref(ty.name))} aria-label={t('dashboard.newType', { type: ty.label })}><Icon name="plus" size={14} />{t('dashboard.new')}</a>
            </li>
          {/each}
        </ul>
      {/if}
    </Card>

    {#each ext.dashboard() as w, i (w.tag + i)}
      <Card class="h-lg-span-6" title={w.title} data-dashboard-widget={w.tag}>
        <Extension tag={w.tag} />
      </Card>
    {/each}
  </div>
  <Slot at="dashboard.after" />
</div>
