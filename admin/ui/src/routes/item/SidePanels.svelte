<script lang="ts">
  import { api } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { channels } from '../../lib/channels.svelte';
  import { config } from '../../lib/config';
  import type { Form } from '../../lib/form.svelte';
  import { formatDateTime } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { href, link } from '../../lib/router.svelte';
  import { types, type CompiledType } from '../../lib/types.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Switch from '../../lib/ui/Switch.svelte';
  import Tabs from '../../lib/ui/Tabs.svelte';
  import LocaleDots from '../collection/LocaleDots.svelte';
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

  let tab = $state('appears');
  const tabs = $derived([
    { id: 'appears', label: t('collections.panel.appears') },
    { id: 'usedBy', label: t('collections.panel.usedBy'), disabled: !data },
    { id: 'versions', label: t('collections.panel.versions'), disabled: !data },
    { id: 'info', label: t('collections.panel.info') },
    ...ext.panels(type.name).map((p, i) => ({ id: `x:${i}`, label: p.title ?? p.tag, disabled: !data })),
  ]);
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

<div class="h-card h-panels">
  <Tabs {tabs} bind:value={tab} label={t('collections.panels')}>
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
        <div class="h-stack">
          <h2 class="h-panels__h">{t('collections.appears.channels')}</h2>
          {#if serving.length === 0}
            <p class="h-muted">{t('collections.appears.none')}</p>
          {:else}
            <ul class="h-panels__list">
              {#each serving as c (c.name)}
                <li>
                  <div class="h-row">
                    <strong>{c.name}</strong>
                    {#if c.access === 'token'}<Badge>{t('collections.appears.private')}</Badge>{/if}
                  </div>
                  {#if c.frontend}<span class="h-muted">{c.frontend}</span>{/if}
                  {#if item}
                    <LocaleDots locales={c.languages} {item} />
                  {:else}
                    <span class="h-muted">{c.languages.join(', ')}</span>
                  {/if}
                </li>
              {/each}
            </ul>
          {/if}

          {#if showPaths}
            <h2 class="h-panels__h">{t('collections.appears.urls')}</h2>
            {#if rowsState === 'loading'}
              <Skeleton lines={2} />
            {:else if rows.length === 0}
              <p class="h-muted">{t('collections.appears.noUrls')}</p>
            {:else}
              <ul class="h-panels__list">
                {#each rows as r (r.locale + r.path + r.state)}
                  <li>
                    <div class="h-row">
                      <code>{frontendOf(r).replace(/\/$/, '')}{r.path}</code>
                      <Badge tone={r.state === 'current' && item?.locales.includes(r.locale) ? 'success' : 'neutral'}>{stateOf(r)}</Badge>
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
          <ul class="h-panels__list">
            {#each refs as r (r.id + r.field)}
              <li>
                <a use:link href={href(`/items/${r.id}`)}>{r.name || r.id}</a>
                <span class="h-muted">{types.get(r.type)?.label ?? r.type} · {r.field}</span>
                <Badge tone={r.onDelete === 'block' ? 'danger' : 'neutral'}>{t(`collections.onDelete.${r.onDelete}`)}</Badge>
              </li>
            {/each}
            {#if hidden > 0}<li class="h-muted">{t('collections.hiddenRefs', { n: hidden })}</li>{/if}
          </ul>
        {/if}
      {:else if active === 'versions'}
        {#if versionsLoading}
          <Skeleton lines={3} />
        {:else if versionsError}
          <p class="h-alert" role="alert">{versionsError}</p>
        {:else if versions.length === 0}
          <p class="h-muted">{t('collections.versions.none')}</p>
        {:else}
          <ul class="h-panels__list">
            {#each versions as v (v.version)}
              <li>
                <div class="h-row">
                  <strong>{t('collections.versions.n', { n: v.version })}</strong>
                  {#if data && v.version === data.version}<Badge tone="success">{t('collections.versions.current')}</Badge>{/if}
                </div>
                <span class="h-muted">{formatDateTime(v.createdAt)} · {v.createdBy}</span>
                <span class="h-muted">{v.locales.join(', ')}</span>
                {#if !editLocked && !(data && v.version === data.version)}
                  <div><Button size="sm" loading={restoring === v.version} onclick={() => restore(v.version)}>{t('collections.versions.restore')}</Button></div>
                {/if}
              </li>
            {/each}
          </ul>
          <p class="h-muted h-panels__note">{t('collections.versions.restoreNote')}</p>
        {/if}
      {:else}
        <dl class="h-panels__info">
          {#if data}
            <dt>{t('collections.info.id')}</dt>
            <dd><code>{data.id}</code></dd>
          {/if}
          <dt>{t('collections.info.type')}</dt>
          <dd>{type.label} <code>{type.name}</code></dd>
          <dt>{t('collections.info.mainLocale')}</dt>
          <dd>{data?.mainLocale ?? locale}</dd>
          {#if data}
            <dt>{t('collections.info.version')}</dt>
            <dd>{data.version || '—'}{#if data.publishedAt} · {formatDateTime(data.publishedAt)}{/if}</dd>
            <dt>{t('collections.info.created')}</dt>
            <dd>{formatDateTime(data.createdAt)}</dd>
            <dt>{t('collections.info.updated')}</dt>
            <dd>{formatDateTime(data.updatedAt)}</dd>
          {/if}
        </dl>
        <div class="h-stack">
          <Field label={t('collections.info.key')} description={keyLocked ? undefined : t('collections.info.keyHelp')} error={form.error('key')}>
            {#snippet children(c)}
              <Input {...c} bind:value={form.values.key} disabled={keyLocked} autocomplete="off" spellcheck={false} />
            {/snippet}
          </Field>
          <Switch bind:checked={form.values.alwaysAvailable} disabled={editLocked} label={t('collections.info.alwaysAvailable')} />
          <p class="h-muted">{t('collections.info.alwaysAvailableHelp')}</p>
        </div>
      {/if}
    {/snippet}
  </Tabs>
</div>

<style>
  .h-panels__h {
    margin: 0;
    font-size: var(--h-text-md);
  }
  .h-panels__list {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: var(--h-text-sm);
  }
  .h-panels__list li {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .h-panels__info {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--h-space-1) var(--h-space-3);
    margin: 0 0 var(--h-space-4);
    font-size: var(--h-text-sm);
  }
  .h-panels__info dt {
    color: var(--h-text-muted);
  }
  .h-panels__info dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .h-panels__note {
    margin-top: var(--h-space-3);
    font-size: var(--h-text-sm);
  }
</style>
