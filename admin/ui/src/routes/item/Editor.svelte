<script lang="ts">
  import '../../lib/messages/collections';
  import { api, ApiError } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { channels } from '../../lib/channels.svelte';
  import { provideFields } from '../../lib/fields/context';
  import Fields from '../../lib/fields/Fields.svelte';
  import { untrack } from 'svelte';
  import { copyTranslatable, fieldsPatch, hydrate, initialValues, cleanDoc, unroutedErrors, type Doc } from '../../lib/fields/logic';
  import { itemLocales, knownLocales, localeState } from '../../lib/fields/locales';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import { types } from '../../lib/types.svelte';
  import Breadcrumbs from '../../lib/ui/Breadcrumbs.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Menu, { type MenuItem } from '../../lib/ui/Menu.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import StatusBadge from '../collection/StatusBadge.svelte';
  import ActionDialogs from './ActionDialogs.svelte';
  import SidePanels from './SidePanels.svelte';
  import { errorMessage } from '../../lib/errors';
  import { formatDateTime } from '../../lib/format';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import { isConflict, isNotFound, itemApi, type Concurrency, type ItemData, type Target } from './model';

  let {
    mode,
    id = '',
    typeName = '',
    embedded = false,
  }: { mode: 'item' | 'new' | 'singleton'; id?: string; typeName?: string; embedded?: boolean } = $props();

  interface Values {
    fields: Doc;
    key: string;
    alwaysAvailable: boolean;
  }

  let data = $state.raw<ItemData | null>(null);
  let loading = $state(true);
  let loadError = $state<Error | null>(null);
  let reloadTick = $state(0);
  // Optimistic concurrency: the ETag of the version this form was loaded from, sent as If-Match on writes.
  let etag = '';
  let gotTag = false;
  const cc = (noMatch = false): Concurrency => ({
    ifMatch: noMatch ? undefined : etag || undefined,
    onHeaders: (h) => {
      const e = h.get('ETag');
      if (e) {
        etag = e;
        gotTag = true;
      }
    },
  });
  let conflict = $state<{ who: string; when: string; retry: () => void } | null>(null);
  function openConflict(e: ApiError, retry: () => void) {
    const cur = e.body?.current as { updatedAt?: string; updatedBy?: string } | undefined;
    if (e.etag) etag = e.etag;
    form.formError = '';
    conflict = { who: cur?.updatedBy || t('collections.conflict.someone'), when: formatDateTime(cur?.updatedAt), retry };
  }
  // A write response that carried no ETag: read it back.
  async function ensureTag() {
    if (gotTag || !target) return;
    await itemApi.read(target, locale, undefined, cc()).catch(() => undefined);
  }
  let saved = $state.raw<Values>({ fields: {}, key: '', alwaysAvailable: false });

  const form = new Form<Values>({ fields: {}, key: '', alwaysAvailable: false }, { prefix: 'fields.' });
  form.guard();

  // The draft view lists draft-only locales too; the published ones decide the state shown per locale.
  let published = $state<string[]>([]);
  $effect(() => {
    const d = data;
    if (!d || d.status === 'draft') {
      published = [];
      return;
    }
    if (!d.draftParts.some((p) => p !== 'shared')) {
      published = d.locales;
      return;
    }
    const ctl = new AbortController();
    api
      .get('/items/{id}', { params: { id: d.id }, query: { locale: d.mainLocale } as never, signal: ctl.signal })
      .then((v) => (published = v.locales))
      .catch(() => (published = d.locales.filter((l) => !d.draftParts.includes(l))));
    return () => ctl.abort();
  });
  const item = $derived(data ? { ...data, locales: published } : null);

  const urlLocale = $derived(router.query.get('locale') ?? '');
  const type = $derived(types.get(mode === 'item' ? (data?.type ?? '') : typeName));
  const isNew = $derived(mode === 'new' || data === null);
  const locale = $derived(data?.locale ?? (urlLocale || knownLocales()[0] || ''));
  const target = $derived<Target | null>(mode === 'item' ? { kind: 'item', id } : mode === 'singleton' ? { kind: 'singleton', type: typeName } : null);
  const canEdit = $derived(isNew ? session.can('content.create', { type: type?.name }) : (data?.actions?.edit ?? true));
  const locales = $derived(item ? itemLocales(knownLocales(), item) : [locale].filter(Boolean));
  const hasTranslatable = $derived(!!type?.fields.some((f) => f.translatable));

  provideFields({
    error: (p) => form.error(p),
    clearError: (p) => form.clearError(p),
    get readonly() {
      return !canEdit;
    },
    get locale() {
      return locale;
    },
    get isNew() {
      return isNew;
    },
    get namePattern() {
      return type?.namePattern;
    },
  });

  function apply(d: ItemData) {
    data = d;
    const t0 = types.get(d.type);
    const v: Values = { fields: hydrate(t0?.fields ?? [], d.fields), key: d.key ?? '', alwaysAvailable: d.alwaysAvailable };
    form.reset(v);
    saved = structuredClone(v);
  }
  function startNew() {
    data = null;
    const v: Values = { fields: hydrate(type?.fields ?? [], initialValues(type?.fields ?? [])), key: '', alwaysAvailable: false };
    form.reset(v);
    saved = structuredClone(v);
  }

  // ---- load
  $effect(() => {
    void reloadTick;
    const loc = urlLocale;
    const tgt = target;
    if (mode === 'item' && !id) return;
    if (mode === 'new' || !tgt) {
      void types.load().then(() => {
        loading = false;
        startNew();
      });
      return;
    }
    const ctl = new AbortController();
    if (!untrack(() => data)) loading = true;
    loadError = null;
    itemApi
      .read(tgt, loc, ctl.signal, cc())
      .then((d) => {
        apply(d);
        loading = false;
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        loading = false;
        if (mode === 'singleton' && isNotFound(e)) startNew();
        else loadError = e;
      });
    return () => ctl.abort();
  });

  // ---- save
  async function save(force = false): Promise<boolean> {
    if (!type || !canEdit) return false;
    let result: ItemData | undefined;
    let clash: ApiError | undefined;
    gotTag = false;
    const ok = await form.submit(async (v) => {
      const body: Record<string, unknown> = {};
      if (locale) body.locale = locale;
      if (data) {
        const patch = fieldsPatch(type.fields, saved.fields, v.fields);
        if (v.key !== saved.key) body.key = v.key;
        if (v.alwaysAvailable !== saved.alwaysAvailable) body.alwaysAvailable = v.alwaysAvailable;
        if (!Object.keys(patch).length && !('key' in body) && !('alwaysAvailable' in body)) {
          // Only normalisation differed: nothing to send, the form is clean.
          saved = structuredClone(v);
          return;
        }
        body.fields = patch;
        try {
          result = await itemApi.update(target!, body, cc(force));
        } catch (e) {
          if (isConflict(e)) clash = e as ApiError;
          throw e;
        }
      } else {
        body.fields = cleanDoc(type.fields, v.fields);
        if (v.key) body.key = v.key;
        if (v.alwaysAvailable) body.alwaysAvailable = true;
        result = mode === 'singleton' ? await itemApi.update(target!, body, cc(true)) : await itemApi.create(type.name, body, cc());
      }
    });
    if (!ok) {
      if (clash) openConflict(clash, () => void save(true));
      return false;
    }
    if (result) {
      const created = mode === 'new';
      apply(result);
      await ensureTag();
      toast.success(t(created ? 'collections.created' : 'collections.saved'));
      if (created) void router.navigate(`/items/${result.id}${result.locale ? `?locale=${encodeURIComponent(result.locale)}` : ''}`, { replace: true, force: true });
    }
    return true;
  }

  function onkey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
      e.preventDefault();
      if (form.dirty || isNew) void save();
    }
  }
  $effect(() => {
    addEventListener('keydown', onkey);
    return () => removeEventListener('keydown', onkey);
  });

  // ---- locales
  function switchLocale(l: string) {
    if (l === locale) return;
    if (form.dirty && !confirm(t('form.unsaved'))) return;
    router.setQuery({ locale: l });
  }
  let copying = $state(false);
  async function copyFromMain() {
    if (!data || !type) return;
    copying = true;
    try {
      const main = await itemApi.read(target!, data.mainLocale);
      const before = JSON.stringify(form.values.fields);
      form.values.fields = copyTranslatable(type.fields, form.values.fields, hydrate(type.fields, main.fields));
      toast.info(JSON.stringify(form.values.fields) === before ? t('collections.copyNothing') : t('collections.copied', { locale: data.mainLocale }));
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      copying = false;
    }
  }

  // ---- preview
  const previewChannels = $derived(
    channels.items.filter((c) => (c.collections == null || c.collections.includes(type?.name ?? '')) && (!channels.active || c.name === channels.current)),
  );
  const previewReason = $derived(
    !data ? t('collections.previewSave') : !session.can('content.preview', { type: type?.name }) ? t('collections.previewForbidden') : previewChannels.length === 0 ? t('collections.previewNoChannel') : '',
  );
  async function preview(channel: string) {
    if (!data) return;
    const c = channels.items.find((x) => x.name === channel);
    const w = window.open('about:blank', '_blank');
    try {
      const r = await api.post('/preview-tokens', {
        body: { channel, item: data.id, ...(c?.languages.includes(locale) ? { locale } : {}) },
      });
      if (r.url) {
        if (w) {
          w.opener = null;
          w.location.href = r.url;
        } else window.open(r.url, '_blank', 'noopener');
      } else {
        w?.close();
        toast.info(t('collections.noPreviewUrl', { channel }));
      }
    } catch (e) {
      w?.close();
      toast.error(e instanceof ApiError && e.status === 403 ? errorMessage(e) : t('collections.previewFailed'));
    }
  }
  const previewMenu = $derived<MenuItem[]>(previewChannels.map((c) => ({ label: c.name, icon: 'eye', onselect: () => void preview(c.name) })));

  // ---- dialogs
  let dialogs = $state<ActionDialogs>();
  async function openPublish() {
    if (form.dirty && !(await save())) return;
    dialogs?.publish();
  }

  const unrouted = $derived(type ? unroutedErrors(type.fields, form.errors, form.values.fields) : {});
  const title = $derived(isNew ? t('collections.newTitle', { type: type?.label ?? '' }) : data?.name || t('collections.untitled'));
  const crumbs = $derived(
    mode === 'singleton'
      ? [{ label: t('nav.settingsContent') }, { label: type?.label ?? typeName }]
      : [{ label: t('nav.content') }, { label: type?.label ?? '', href: type ? `/collections/${type.name}` : undefined }],
  );
  const canPublish = $derived(!isNew && (data?.actions?.publish ?? true) && (data?.draftParts.length ?? 0) > 0);
  const canDiscard = $derived(!isNew && canEdit && (data?.draftParts.length ?? 0) > 0);
  const canDelete = $derived(!isNew && mode !== 'singleton' && (data?.actions?.delete ?? true));
</script>

{#if loadError}
  <div class="h-page">
    {#if isNotFound(loadError)}
      <EmptyState icon="file" title={t('collections.itemNotFound')} description={t('collections.itemNotFoundText')}>
        {#snippet actions()}<a class="h-button h-button--secondary h-button--md" use:link href={href('/')}>{t('nav.dashboard')}</a>{/snippet}
      </EmptyState>
    {:else}
      <div class="h-alert" role="alert">
        <span>{errorMessage(loadError)}</span>
        <Button size="sm" onclick={() => reloadTick++}>{t('app.retry')}</Button>
      </div>
    {/if}
  </div>
{:else if loading || !type}
  <div class="h-page" aria-busy="true">
    <Skeleton height="2rem" width="40%" />
    <Skeleton lines={6} />
  </div>
{:else}
  <div class="h-page h-item">
    {#if !embedded}<Breadcrumbs items={crumbs} />{/if}
    <div class="h-page__head">
      <div class="h-item__title">
        {#if embedded}<h2>{title}</h2>{:else}<h1>{title}</h1>{/if}
        <div class="h-row">
          <span class="h-muted">{type.label}</span>
          {#if data}<StatusBadge status={data.status} />{:else}<StatusBadge status="draft" />{/if}
          {#if form.dirty}<span class="h-muted" role="status">{t('collections.unsavedBadge')}</span>{/if}
        </div>
      </div>
      <div class="h-row h-item__actions">
        {#if previewReason}
          <span title={previewReason}>
            <Button icon="eye" disabled>{t('collections.preview.button')}</Button>
            <span class="h-sr">{previewReason}</span>
          </span>
        {:else if previewMenu.length === 1}
          <Button icon="eye" onclick={() => void preview(previewChannels[0]!.name)}>{t('collections.preview.button')}</Button>
        {:else}
          <Menu items={previewMenu} label={t('collections.preview.choose')}>
            {#snippet trigger()}{t('collections.preview.button')}{/snippet}
          </Menu>
        {/if}
        {#if canDiscard}<Button onclick={() => dialogs?.discard()}>{t('collections.discard')}</Button>{/if}
        {#if canDelete}<Button variant="danger" icon="trash" onclick={() => dialogs?.remove()}>{t('common.delete')}</Button>{/if}
        <Button variant="primary" loading={form.pending} disabled={!canEdit || (!form.dirty && !isNew)} onclick={() => void save()}>{t('common.save')}</Button>
        <Button variant="primary" disabled={isNew || (!canPublish && !form.dirty)} onclick={() => void openPublish()}>{t('collections.publish')}</Button>
      </div>
    </div>

    <div class="h-item__locales h-row" role="group" aria-label={t('collections.languages')}>
      {#each locales as l (l)}
        {@const s = item ? localeState(l, item) : l === locale ? 'draft' : 'missing'}
        <button type="button" class="h-item__locale" aria-pressed={l === locale} disabled={isNew && l !== locale} onclick={() => switchLocale(l)}>
          <span class="h-item__locale-code">{l}</span>
          <span class="h-item__locale-state">{t(`collections.locale.${s}`)}</span>
        </button>
      {/each}
      {#if data && locale !== data.mainLocale && hasTranslatable && canEdit}
        <Button size="sm" loading={copying} onclick={() => void copyFromMain()}>{t('collections.copyFrom', { locale: data.mainLocale })}</Button>
      {/if}
    </div>

    {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
    {#if Object.keys(unrouted).length}
      <div class="h-alert h-item__unrouted" role="alert">
        <ul>
          {#each Object.entries(unrouted) as [path, msg] (path)}<li><strong>{path}</strong>: {msg}</li>{/each}
        </ul>
      </div>
    {/if}
    {#if !canEdit}<p class="h-muted" role="status">{t('collections.readOnly')}</p>{/if}

    <div class="h-item__grid">
      <form
        class="h-card h-item__form"
        novalidate
        aria-label={t('collections.formLabel', { type: type.label })}
        onsubmit={(e) => {
          e.preventDefault();
          void save();
        }}
      >
        <Fields fields={type.fields} bind:doc={form.values.fields} />
        {#if type.fields.length === 0}<p class="h-muted">{t('collections.noFields')}</p>{/if}
      </form>
      <aside class="h-item__side" aria-label={t('collections.details')}>
        <SidePanels {data} {item} {type} {locale} {locales} {form} {mode} />
      </aside>
    </div>
  </div>

  <ActionDialogs
    bind:this={dialogs}
    {data}
    {type}
    {locale}
    {published}
    {cc}
    onconflict={openConflict}
    onchanged={() => reloadTick++}
    onswitch={(l) => router.setQuery({ locale: l })}
    onerrors={(f) => {
      form.setErrors(f);
      form.formError = t('form.invalid');
    }}
  />
{/if}

<Dialog open={!!conflict} title={t('collections.conflict.title')} size="sm" dismissible={false}>
  {#if conflict}
    <p>{t('collections.conflict.text', { who: conflict.who, when: conflict.when })}</p>
    <p class="h-muted">{t('collections.conflict.hint')}</p>
  {/if}
  {#snippet footer()}
    <Button
      onclick={() => {
        conflict = null;
        reloadTick++;
      }}>{t('collections.conflict.reload')}</Button>
    <Button
      variant="danger"
      onclick={() => {
        const r = conflict?.retry;
        conflict = null;
        r?.();
      }}>{t('collections.conflict.overwrite')}</Button>
  {/snippet}
</Dialog>

<style>
  .h-item__title {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-1);
    min-width: 0;
  }
  .h-item__title h1 {
    overflow-wrap: anywhere;
  }
  .h-item__actions {
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .h-page__head {
    align-items: flex-start;
    flex-wrap: wrap;
  }
  .h-item__grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 22rem;
    gap: var(--h-space-4);
    align-items: start;
  }
  @media (max-width: 70rem) {
    .h-item__grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  .h-item__form {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-4);
  }
  .h-item__side {
    min-width: 0;
  }
  .h-item__locales {
    flex-wrap: wrap;
  }
  .h-item__locale {
    display: inline-flex;
    flex-direction: column;
    align-items: flex-start;
    padding: var(--h-space-1) var(--h-space-3);
    border: 1px solid var(--h-border-strong);
    border-radius: var(--h-radius);
    background: var(--h-surface);
    color: var(--h-text);
    font: inherit;
    line-height: 1.2;
    cursor: pointer;
  }
  .h-item__locale[aria-pressed='true'] {
    border-color: var(--h-accent);
    background: var(--h-accent-soft);
  }
  .h-item__locale:disabled {
    cursor: not-allowed;
    opacity: 0.55;
  }
  .h-item__locale-code {
    font-weight: 600;
  }
  .h-item__locale-state {
    color: var(--h-text-muted);
    font-size: var(--h-text-xs);
  }
  .h-item__unrouted ul {
    margin: 0;
    padding-left: var(--h-space-4);
  }
</style>
