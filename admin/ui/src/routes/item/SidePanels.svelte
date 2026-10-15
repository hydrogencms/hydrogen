<script lang="ts">
  import { api } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { channels } from '../../lib/channels.svelte';
  import { config } from '../../lib/config';
  import type { Form } from '../../lib/form.svelte';
    import { t } from '../../lib/i18n';
  import { href, link } from '../../lib/router.svelte';
  import { types, type CompiledType } from '../../lib/types.svelte';
  import '../../lib/messages/editor';
  import Badge from '../../lib/ui/Badge.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Menu from '../../lib/ui/Menu.svelte';
  import NameCell from '../../lib/ui/NameCell.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Switch from '../../lib/ui/Switch.svelte';
  import Tabs from '../../lib/ui/Tabs.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import { localeState } from '../../lib/fields/locales';
  import { copyText } from '../../lib/errors';
  import StatusBadge from '../collection/StatusBadge.svelte';
  import { actorName } from './actors';
  import { hydrate, type Doc } from '../../lib/fields/logic';
  import Button from '../../lib/ui/Button.svelte';
  import { ext } from '../../lib/extensions.svelte';
  import Extension from '../../lib/ext/Extension.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import type { ItemData, Referrer } from './model';

  let {
    data,
    item,
    type,
    locale,
    locales,
    form,
    mode,
    onrefresh,
  }: {
    data: ItemData | null;
    /** `data` with `locales` limited to the published ones. */
    item: ItemData | null;
    type: CompiledType;
    locale: string;
    locales: string[];
    form: Form<{ fields: Doc; key: string; alwaysAvailable: boolean }>;
    mode: 'item' | 'new' | 'singleton';
    onrefresh?: () => void;
  } = $props();

  let tab = $state('info');
  const allTabs = $derived([
    { id: 'info', label: t('editor.tab.details') },
    { id: 'appears', label: t('editor.tab.appears') },
    { id: 'usedBy', label: t('editor.tab.references'), disabled: !data },
    { id: 'versions', label: t('editor.tab.history'), disabled: !data },
    ...ext.panels(type.name).map((p, i) => ({ id: `x:${i}`, label: p.title ?? p.tag, disabled: !data })),
  ]);
  // More than five panels: the first four stay as segments, the rest move into a menu (the open one stays visible).
  const MAX = 5;
  const shownTabs = $derived(
    allTabs.length <= MAX ? allTabs : [...allTabs.slice(0, MAX - 1), ...allTabs.slice(MAX - 1).filter((x) => x.id === tab)],
  );
  const moreTabs = $derived(allTabs.length <= MAX ? [] : allTabs.slice(MAX - 1).filter((x) => x.id !== tab));
  const extPanels = $derived(ext.panels(type.name));

  // ---- where this appears
  const serving = $derived(channels.items.filter((c) => c.collections == null || c.collections.includes(type.name)));
  interface Row {
    locale: string;
    path: string;
    site: string;
    state: 'current' | 'alias' | 'redirect' | 'manual';
    status?: number;
    target?: string;
  }
  let rows = $state<Row[]>([]);
  let rowsState = $state<'idle' | 'loading' | 'done'>('idle');
  const showPaths = $derived(config().pages && session.can('pages.read') && !!data);
  $effect(() => {
    const id = data?.id;
    const v = data?.version;
    void v;
    if (!id || !showPaths) {
      rows = [];
      return;
    }
    const ctl = new AbortController();
    rowsState = 'loading';
    api
      .get('/pages/items/{id}/paths', { params: { id }, signal: ctl.signal })
      .then((r) => {
        rows = r.items as Row[];
        rowsState = 'done';
      })
      .catch(() => {
        if (!ctl.signal.aborted) {
          rows = [];
          rowsState = 'done';
        }
      });
    return () => ctl.abort();
  });
  const dotOf = (l: string) => {
    const s = item ? localeState(l, item) : l === locale ? 'draft' : 'missing';
    return s === 'published' ? 'filled' : s === 'draft' ? 'half' : 'hollow';
  };
  const frontendOf = (r: Row) =>
    channels.items.find((c) => (c.options as { site?: string }).site === r.site && c.languages.includes(r.locale))?.frontend ?? '';
  const stateOf = (r: Row) => {
    if (r.state !== 'current') return t(`collections.route.${r.state}`);
    return item?.locales.includes(r.locale) ? t('collections.route.live') : t('collections.route.draftOnly');
  };

  // ---- used by
  let refs = $state<Referrer[]>([]);
  let hidden = $state(0);
  let refsLoading = $state(false);
  let refsError = $state('');
  $effect(() => {
    const id = tab === 'usedBy' ? data?.id : undefined;
    if (!id) return;
    const ctl = new AbortController();
    refsLoading = true;
    refsError = '';
    api
      .get('/items/{id}/references', { params: { id }, signal: ctl.signal })
      .then((r) => {
        refs = r.items as Referrer[];
        hidden = r.hidden ?? 0;
        refsLoading = false;
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        refsError = e.message;
        refsLoading = false;
      });
    return () => ctl.abort();
  });

  // ---- versions
  interface Version {
    version: number;
    createdAt: string;
    createdBy: string;
    locales: string[];
  }
  let versions = $state<Version[]>([]);
  let versionsLoading = $state(false);
  let versionsError = $state('');
  $effect(() => {
    const id = tab === 'versions' ? data?.id : undefined;
    void data?.version;
    if (!id) return;
    const ctl = new AbortController();
    versionsLoading = true;
    versionsError = '';
    api
      .get('/items/{id}/versions', { params: { id }, signal: ctl.signal })
      .then((r) => {
        versions = r.items as Version[];
        versionsLoading = false;
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        versionsError = e.message;
        versionsLoading = false;
      });
    return () => ctl.abort();
  });

  // Who made each version, resolved once per list.
  let who = $state<Record<string, string>>({});
  $effect(() => {
    const list = versions;
    let live = true;
    for (const by of new Set(list.map((v) => v.createdBy))) {
      void actorName(by).then((n) => live && (who = { ...who, [by]: n }));
    }
    return () => (live = false);
  });

  async function copyId() {
    if (data && (await copyText(data.id))) toast.success(t('editor.idCopied'));
  }

  let restoring = $state(0);
  // Copies a version's shared fields and the current locale's fields into the form as unsaved changes.
  async function restore(n: number) {
    if (!data || restoring) return;
    if (form.dirty && !confirm(t('collections.restore.confirm'))) return;
    restoring = n;
    try {
      const v = await api.get('/items/{id}/versions/{n}', { params: { id: data.id, n } });
      const shared = ((v.shared as { fields?: Doc } | undefined)?.fields ?? {}) as Doc;
      const loc = (v.locales as Record<string, { fields?: Doc }> | undefined)?.[locale];
      // Merge: shared fields always; translatable fields only when the version has this locale.
      const next: Doc = { ...form.values.fields };
      for (const f of type.fields) {
        const src = f.translatable ? (loc ? (loc.fields ?? {}) : undefined) : shared;
        if (!src) continue;
        if (src[f.name] !== undefined) next[f.name] = src[f.name];
        else delete next[f.name];
      }
      form.values.fields = hydrate(type.fields, next);
      const noLocale = !loc && type.fields.some((f) => f.translatable);
      toast.info(noLocale ? `${t('collections.versions.restored', { n })} ${t('collections.restore.noLocale', { locale })}` : t('collections.versions.restored', { n }));
    } catch (e) {
      toast.error((e as Error).message);
    } finally {
      restoring = 0;
    }
  }

  const editLocked = $derived(!!data?.actions && !data.actions.edit);
  const keyLocked = $derived(mode === 'singleton' || editLocked);
</script>

<div class="h-panels">
  <Tabs tabs={shownTabs} bind:value={tab} label={t('collections.panels')} variant="segmented">
    {#snippet trailing()}
      {#if moreTabs.length}
        <Menu
          label={t('editor.morePanels')}
          items={moreTabs.map((x) => ({ label: x.label, disabled: x.disabled, onselect: () => (tab = x.id) }))}
        />
      {/if}
    {/snippet}
    {#snippet children(active)}
      {#if active.startsWith('x:')}
        {@const p = extPanels[Number(active.slice(2))]}
        {#if p && data}
          <Extension
            tag={p.tag}
            props={{ item: data, type: type.name, locale, draft: data.status !== 'published' }}
            onEvents={{ refresh: () => onrefresh?.() }}
          />
        {/if}
      {:else if active === 'appears'}
        <div class="h-panels__sec">
          <h3 class="h-panels__h">{t('collections.appears.channels')}</h3>
          {#if serving.length === 0}
            <p class="h-muted">{t('collections.appears.none')}</p>
          {:else}
            <ul class="h-panels__rows">
              {#each serving as c (c.name)}
                <li class="h-panels__row">
                  <div class="h-panels__line">
                    <Icon name="globe" size={16} />
                    <strong>{c.name}</strong>
                    {#if c.access === 'token'}<Badge>{t('collections.appears.private')}</Badge>{/if}
                  </div>
                  {#if c.frontend}<span class="h-muted h-panels__host">{c.frontend}</span>{/if}
                  <ul class="h-panels__langs" aria-label={t('collections.completeness')}>
                    {#each c.languages as l (l)}
                      <li class="h-panels__lang">
                        <span class="h-dot h-dot--{dotOf(l)}" aria-hidden="true"></span>{l}
                        <span class="h-sr">{t(`collections.locale.${item ? localeState(l, item) : 'missing'}`)}</span>
                      </li>
                    {/each}
                  </ul>
                </li>
              {/each}
            </ul>
          {/if}

          {#if showPaths}
            <h3 class="h-panels__h">{t('collections.appears.urls')}</h3>
            {#if rowsState === 'loading'}
              <Skeleton lines={2} />
            {:else if rows.length === 0}
              <p class="h-muted">{t('collections.appears.noUrls')}</p>
            {:else}
              <ul class="h-panels__rows">
                {#each rows as r (r.locale + r.path + r.state)}
                  {@const url = `${frontendOf(r).replace(/\/$/, '')}${r.path}`}
                  {@const live = r.state === 'current' && !!item?.locales.includes(r.locale)}
                  <li class="h-panels__row">
                    <div class="h-panels__line">
                      {#if live && frontendOf(r)}
                        <a class="h-panels__url" href={url} target="_blank" rel="noopener noreferrer"><code>{url}</code><Icon name="external-link" size={14} /></a>
                      {:else}
                        <code class="h-panels__url">{url}</code>
                      {/if}
                      <Badge tone={live ? 'success' : 'neutral'}>{stateOf(r)}</Badge>
                    </div>
                    <span class="h-muted">{r.locale} · {r.site}{#if r.target} → {r.target}{/if}</span>
                  </li>
                {/each}
              </ul>
            {/if}
          {/if}
        </div>
      {:else if active === 'usedBy'}
        {#if refsLoading}
          <Skeleton lines={3} />
        {:else if refsError}
          <p class="h-alert" role="alert">{refsError}</p>
        {:else if refs.length === 0 && hidden === 0}
          <p class="h-muted">{t('collections.usedBy.none')}</p>
        {:else}
          <ul class="h-panels__rows">
            {#each refs as r (r.id + r.field)}
              <li class="h-panels__row h-panels__row--ref">
                <NameCell name={r.name || r.id} sub={`${types.get(r.type)?.label ?? r.type} · ${r.field}`} icon={types.get(r.type)?.admin?.icon || 'file-text'} href={`/items/${r.id}`} />
                <Badge tone={r.onDelete === 'block' ? 'danger' : 'neutral'}>{t(`collections.onDelete.${r.onDelete}`)}</Badge>
              </li>
            {/each}
          </ul>
          {#if hidden > 0}<p class="h-muted h-panels__note">{t('editor.refs.hidden', { n: hidden })}</p>{/if}
        {/if}
      {:else if active === 'versions'}
        {#if versionsLoading}
          <Skeleton lines={3} />
        {:else if versionsError}
          <p class="h-alert" role="alert">{versionsError}</p>
        {:else if versions.length === 0}
          <p class="h-muted">{t('collections.versions.none')}</p>
        {:else}
          <ol class="h-panels__timeline">
            {#each versions as v (v.version)}
              {@const current = !!data && v.version === data.version}
              <li class="h-panels__ver" class:h-panels__ver--current={current}>
                <span class="h-panels__node" aria-hidden="true"></span>
                <div class="h-panels__line">
                  <strong>{t('editor.meta.version', { n: v.version })}</strong>
                  <span>{t('editor.history.published')}</span>
                  {#if current}<Badge tone="success">{t('collections.versions.current')}</Badge>{/if}
                </div>
                <span class="h-muted"><Time at={v.createdAt} />{#if who[v.createdBy]}{' '}{t('editor.history.by', { who: who[v.createdBy] })}{/if}</span>
                {#if v.locales.length}<span class="h-muted">{v.locales.join(', ')}</span>{/if}
                {#if !editLocked && !current}
                  <div class="h-panels__restore"><Button variant="ghost" size="sm" loading={restoring === v.version} onclick={() => restore(v.version)}>{t('collections.versions.restore')}</Button></div>
                {/if}
              </li>
            {/each}
          </ol>
          <p class="h-muted h-panels__note">{t('collections.versions.restoreNote')}</p>
        {/if}
      {:else}
        <div class="h-panels__sec">
          <dl class="h-panels__info">
            <dt>{t('editor.details.status')}</dt>
            <dd><StatusBadge status={data?.status ?? 'draft'} /></dd>
            <dt>{t('editor.details.type')}</dt>
            <dd>{type.label}</dd>
            {#if data}
              <dt>{t('editor.details.version')}</dt>
              <dd class="h-num">{data.version || '—'}</dd>
              <dt>{t('editor.details.created')}</dt>
              <dd><Time at={data.createdAt} /></dd>
              <dt>{t('editor.details.updated')}</dt>
              <dd><Time at={data.updatedAt} /></dd>
              {#if data.publishedAt}
                <dt>{t('editor.details.published')}</dt>
                <dd><Time at={data.publishedAt} /></dd>
              {/if}
              <dt>{t('editor.details.id')}</dt>
              <dd class="h-panels__id"><code title={data.id}>{data.id.length > 20 ? `${data.id.slice(0, 8)}…${data.id.slice(-8)}` : data.id}</code><IconButton icon="copy" size="sm" label={t('editor.copyId')} onclick={() => void copyId()} /></dd>
            {/if}
          </dl>

          <h3 class="h-panels__h">{t('editor.details.languages')}</h3>
          <ul class="h-panels__rows h-panels__rows--tight" aria-label={t('collections.completeness')}>
            {#each locales as l (l)}
              {@const s = item ? localeState(l, item) : l === locale ? 'draft' : 'missing'}
              <li class="h-panels__loc">
                <span class="h-dot h-dot--{dotOf(l)}" aria-hidden="true"></span>
                <span class="h-panels__code">{l}</span>
                {#if data && l === data.mainLocale}<span class="h-muted">{t('editor.details.mainLocale')}</span>{/if}
                <span class="h-spacer"></span>
                <span class="h-muted">{t(`editor.locale.${s}`)}</span>
              </li>
            {/each}
          </ul>

          <h3 class="h-panels__h">{t('editor.details.settings')}</h3>
          <Field label={t('collections.info.key')} description={keyLocked ? undefined : t('collections.info.keyHelp')} error={form.error('key')}>
            {#snippet children(c)}
              <Input {...c} bind:value={form.values.key} disabled={keyLocked} autocomplete="off" spellcheck={false} />
            {/snippet}
          </Field>
          <Switch bind:checked={form.values.alwaysAvailable} disabled={editLocked} label={t('collections.info.alwaysAvailable')} />
          <p class="h-muted h-panels__note">{t('collections.info.alwaysAvailableHelp')}</p>
        </div>
      {/if}
    {/snippet}
  </Tabs>
</div>

<style>
  .h-panels {
    min-width: 0;
  }
  .h-panels :global(.h-tabs--segmented .h-tabs__bar) {
    margin: 0;
  }
  .h-panels__sec {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-3);
  }
  .h-panels__h {
    margin: var(--h-space-2) 0 0;
    font-size: var(--h-text-sm);
    font-weight: 600;
  }
  .h-panels__sec > .h-panels__h:first-child {
    margin-top: 0;
  }
  .h-panels__rows {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: var(--h-text-sm);
  }
  .h-panels__row {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: var(--h-space-3) 0;
    border-bottom: 1px solid var(--h-border);
  }
  .h-panels__row:last-child {
    border-bottom: 0;
  }
  .h-panels__row--ref {
    flex-direction: row;
    align-items: center;
    justify-content: space-between;
    gap: var(--h-space-2);
  }
  .h-panels__line {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--h-space-2);
  }
  .h-panels__host {
    overflow-wrap: anywhere;
  }
  .h-panels__langs {
    display: flex;
    flex-wrap: wrap;
    gap: var(--h-space-1);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .h-panels__lang {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    height: 22px;
    padding: 0 var(--h-space-2);
    border-radius: var(--h-radius-pill);
    background: var(--h-surface-2);
    font-size: var(--h-text-xs);
    font-weight: 500;
  }
  .h-panels__url {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .h-panels__url code {
    font-size: var(--h-text-xs);
  }
  .h-panels__rows--tight {
    gap: 2px;
  }
  .h-panels__loc {
    display: flex;
    align-items: center;
    gap: var(--h-space-2);
    min-height: 2rem;
  }
  .h-panels__code {
    font-weight: 500;
  }
  .h-panels__info {
    display: grid;
    grid-template-columns: max-content 1fr;
    align-items: center;
    gap: var(--h-space-2) var(--h-space-4);
    margin: 0;
    font-size: var(--h-text-sm);
  }
  .h-panels__info dt {
    color: var(--h-text-muted);
  }
  .h-panels__info dd {
    display: flex;
    align-items: center;
    gap: var(--h-space-1);
    min-width: 0;
    margin: 0;
    overflow-wrap: anywhere;
  }
  .h-panels__id code {
    min-width: 0;
    overflow: hidden;
    font-size: var(--h-text-xs);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .h-panels__timeline {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: var(--h-space-4);
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: var(--h-text-sm);
  }
  .h-panels__timeline::before {
    content: '';
    position: absolute;
    top: 8px;
    bottom: 8px;
    left: 4px;
    width: 1px;
    background: var(--h-border-strong);
  }
  .h-panels__ver {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding-left: var(--h-space-5);
  }
  .h-panels__node {
    position: absolute;
    top: 5px;
    left: 0;
    width: 9px;
    height: 9px;
    border: 2px solid var(--h-border-strong);
    border-radius: 50%;
    background: var(--h-surface);
  }
  .h-panels__ver--current .h-panels__node {
    border-color: var(--h-success);
    background: var(--h-success);
  }
  .h-panels__restore {
    margin-left: calc(var(--h-space-3) * -1);
  }
  .h-panels__note {
    margin-top: var(--h-space-3);
    font-size: var(--h-text-xs);
  }
</style>
