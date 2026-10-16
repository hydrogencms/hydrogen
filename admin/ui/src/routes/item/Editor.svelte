<script lang="ts">
  import '../../lib/messages/collections';
  import '../../lib/messages/editor';
  import { api, ApiError } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { channels } from '../../lib/channels.svelte';
  import { provideFields } from '../../lib/fields/context';
  import Fields from '../../lib/fields/Fields.svelte';
  import { untrack, type Component } from 'svelte';
  import '../../lib/messages/preview';
  import { copyTranslatable, fieldsPatch, hydrate, initialValues, cleanDoc, unroutedErrors, type Doc } from '../../lib/fields/logic';
  import { itemLocales, knownLocales, localeState } from '../../lib/fields/locales';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import { types } from '../../lib/types.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Card from '../../lib/ui/Card.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import Menu, { type MenuItem } from '../../lib/ui/Menu.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Segmented from '../../lib/ui/Segmented.svelte';
  import { Relay, type RelayState } from '../../lib/preview/relay';
  import { relayApi } from '../../lib/preview/share';
  import { ShareList } from '../../lib/preview/shares.svelte';
  import type { PreviewChannel } from '../../lib/preview/PreviewPane.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { ext, navigateTo } from '../../lib/extensions.svelte';
  import Extension from '../../lib/ext/Extension.svelte';
  import Slot from '../../lib/ext/Slot.svelte';
  import StatusBadge from '../collection/StatusBadge.svelte';
  import ActionDialogs from './ActionDialogs.svelte';
  import '../../lib/messages/releases';
  import { releaseApi } from '../release/api';
  import { formatInZone, nextScheduled, type Release } from '../release/logic';
  import SidePanels from './SidePanels.svelte';
  import '../../lib/messages/review';
  import { reviewApi } from '../review/api';
  import { canBypass as showBypass, publishBlock, stateTone, type ReviewData } from '../review/logic';
  import ReviewActions from '../review/ReviewActions.svelte';
  import { reviewRev } from '../review/revision.svelte';
  import { copyText, errorMessage } from '../../lib/errors';
  import { formatDateTime } from '../../lib/format';
  import type { CompiledField } from '../../lib/types.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import { confirmDiscard, isConflict, isNotFound, itemApi, type Concurrency, type ItemData, type Target } from './model';

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
    // Nothing is published for a never-published or unpublished item.
    if (!d || d.status === 'draft' || d.status === 'unpublished') {
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
    errors: () => form.errors,
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
    if ((e.ctrlKey || e.metaKey) && e.shiftKey && !e.altKey && e.key.toLowerCase() === 'p' && !previewReason) {
      e.preventDefault();
      togglePreview();
      return;
    }
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
  function switchLocale(l: string): boolean {
    if (l === locale) return true;
    if (form.dirty && !confirm(t('form.unsaved'))) return false;
    router.setQuery({ locale: l });
    return true;
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
  // The pane: a lazy chunk, opened by the header toggle or Mod-Shift-P.
  let previewOpen = $state(false);
  let paneTab = $state<'preview' | 'details'>('preview');
  let PreviewPane = $state.raw<Component<any> | null>(null);
  let paneMode = $state<'live' | 'reload' | null>(null);
  function togglePreview() {
    previewOpen = !previewOpen;
    paneTab = 'preview';
    if (!previewOpen) paneMode = null;
  }
  $effect(() => {
    if (previewOpen && !PreviewPane) void import('../../lib/preview/PreviewPane.svelte').then((m) => (PreviewPane = m.default));
  });
  const previewChans = $derived<PreviewChannel[]>(previewChannels.map((c) => ({ name: c.name, languages: c.languages })));
  // The working document, as the pane and the relay see it.
  const shares = new ShareList();
  const relayItem = $derived(mode === 'new' ? '' : (data?.id ?? ''));
  const relayWanted = $derived(!!relayItem && canEdit && ((previewOpen && paneMode === 'reload') || shares.active > 0));
  const workingFields = $derived(type && (previewOpen || relayWanted) ? cleanDoc(type.fields, $state.snapshot(form.values.fields) as Doc) : null);
  const previewDoc = $derived(previewOpen && workingFields ? { fields: workingFields } : null);

  $effect(() => {
    const itemId = relayItem;
    if (!itemId || !session.can('content.preview')) return;
    void shares.load(itemId).catch(() => {});
    return () => shares.reset();
  });

  // Unsaved edits go to the server's relay (not to the draft) while a reload-mode frame or a share link shows them.
  // The relayed draft is dropped when the item is left, after a save (the draft is the truth again) and on pagehide.
  let relay = $state.raw<Relay>();
  let relayState = $state<RelayState>('idle');
  $effect(() => {
    const itemId = relayItem;
    const loc = locale;
    if (!itemId) return;
    const r = new Relay({
      put: (f) => relayApi.put(itemId, loc, f),
      del: (keepalive) => relayApi.del(itemId, loc, keepalive),
      onState: (st) => (relayState = st),
    });
    relay = r;
    const hide = () => void r.clear(true);
    addEventListener('pagehide', hide);
    return () => {
      removeEventListener('pagehide', hide);
      relay = undefined;
      relayState = 'idle';
      void r.clear();
    };
  });
  $effect(() => {
    const r = relay;
    if (!r) return;
    if (relayWanted && form.dirty && workingFields) {
      // Just wanted (a frame went to reload mode, a link was made): show the unsaved state at once.
      r.push(workingFields, wantedBy !== r);
      wantedBy = r;
    } else {
      wantedBy = undefined;
      void r.clear();
    }
  });
  let wantedBy: Relay | undefined;
  const flushRelay = () => relay?.flush() ?? Promise.resolve(true);

  // ---- dialogs
  let dialogs = $state<ActionDialogs>();
  async function openPublish() {
    if (form.dirty && !(await save())) return;
    // Saving a draft reopens an approved review: re-read it before publishing.
    if (reviewRequired && data) {
      const r = await reviewApi.get(data.id).catch(() => null);
      if (r) review = r;
      if (r && publishBlock(true, r.state, canBypassReview)) {
        toast.error(t('review.needsApproved'));
        return;
      }
    }
    dialogs?.publish();
  }
  async function withSaved(fn: () => void) {
    if (form.dirty && !(await save())) return;
    fn();
  }

  // Open and scheduled releases that contain the item (the header badge and its link).
  let releases = $state.raw<Release[]>([]);
  let relTick = $state(0);
  let relFor: string | undefined;
  $effect(() => {
    void relTick;
    const id = mode === 'item' ? data?.id : undefined;
    // Another item: drop the previous item's badge now rather than when the new answer lands.
    if (id !== relFor) {
      relFor = id;
      releases = [];
    }
    if (!id || !session.can('content.read')) {
      releases = [];
      return;
    }
    const ctl = new AbortController();
    releaseApi
      .ofItem(id, ctl.signal)
      .then((r) => (releases = r))
      .catch(() => {
        if (!ctl.signal.aborted) releases = [];
      });
    return () => ctl.abort();
  });
  const scheduled = $derived(nextScheduled(releases));

  // ---- review (types with `review: true`)
  let review = $state.raw<ReviewData | null>(null);
  let bypassOpen = $state(false);
  const reviewKey = $derived(`${data?.id ?? ''}|${data?.version ?? ''}|${data?.updatedAt ?? ''}|${data?.status ?? ''}`);
  const reviewRequired = $derived(mode !== 'new' && !!data && !!type?.review);
  const canBypassReview = $derived(session.can('review.bypass', { type: type?.name }));
  $effect(() => {
    void reviewKey;
    void reviewRev.n;
    const rid = reviewRequired ? untrack(() => data?.id) : undefined;
    if (!rid) {
      review = null;
      return;
    }
    const ctl = new AbortController();
    reviewApi
      .get(rid, ctl.signal)
      .then((r) => (review = r))
      .catch(() => {
        if (!ctl.signal.aborted) review = null;
      });
    return () => ctl.abort();
  });
  let bypassBusy = $state(false);
  // Publishes every draft locale at once (the publish dialog's default); the server records the bypass.
  async function publishBypass() {
    if (!data || !target) return;
    if (form.dirty && !(await save())) {
      bypassOpen = false;
      return;
    }
    bypassBusy = true;
    try {
      const locs = (data.draftParts ?? []).filter((p) => p !== 'shared').sort();
      await itemApi.publish(target, locs.length ? { locales: locs } : {}, cc());
      bypassOpen = false;
      toast.success(t('collections.published'));
      reloadTick++;
      relTick++;
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500 || e.status === 0) throw e;
      if (isConflict(e)) {
        bypassOpen = false;
        openConflict(e, () => void publishBypass());
      } else toast.error(errorMessage(e));
    } finally {
      bypassBusy = false;
    }
  }
  const reviewState = $derived(review?.state ?? 'none');
  const needsReview = $derived(reviewRequired && (review?.required ?? true));
  const reviewBlock = $derived(needsReview ? publishBlock(true, reviewState, canBypassReview) : '');
  const bypassItem = $derived(needsReview && showBypass(true, reviewState, canBypassReview));
  const reviewReason = $derived(reviewBlock ? t('review.needsApproved') : '');

  const unrouted = $derived(type ? unroutedErrors(type.fields, form.errors, form.values.fields) : {});
  const title = $derived(isNew ? t('collections.newTitle', { type: type?.label ?? '' }) : data?.name || t('editor.untitled'));
  const crumbs = $derived(
    mode === 'singleton'
      ? [{ label: t('nav.settingsContent') }, { label: type?.label ?? typeName }]
      : [{ label: t('nav.content') }, { label: type?.label ?? '', href: type ? `/collections/${type.name}` : undefined }],
  );
  const canPublish = $derived(!isNew && (data?.actions?.publish ?? true) && ((data?.draftParts.length ?? 0) > 0 || data?.status === 'unpublished'));
  const canDiscard = $derived(!isNew && canEdit && (data?.draftParts.length ?? 0) > 0);
  const extActions = $derived(data && type ? ext.actions(type.name) : []);
  // An extension asked for a reload: unsaved edits are only dropped after a confirmation.
  function extRefresh() {
    if (confirmDiscard(form.dirty, (m) => confirm(m))) reloadTick++;
  }
  const extEvents = {
    refresh: extRefresh,
    navigate: (e: CustomEvent) => navigateTo(e.detail?.path),
  };
  const slotProps = $derived({ item: data ?? undefined, type: type?.name });
  const canDelete = $derived(!isNew && mode !== 'singleton' && (data?.actions?.delete ?? true));

  // ---- layout
  const groups = $derived.by(() => {
    const m = new Map<string, CompiledField[]>();
    for (const f of type?.fields ?? []) {
      if (f.admin?.hidden) continue;
      const g = f.group ?? '';
      m.set(g, [...(m.get(g) ?? []), f]);
    }
    return [...m].map(([g, fields]) => ({ title: g || t('editor.content'), fields }));
  });

  const localeOptions = $derived(
    locales.map((l) => {
      const s = item ? localeState(l, item) : l === locale ? 'draft' : 'missing';
      return { value: l, label: l, dot: s === 'published' ? ('filled' as const) : s === 'draft' ? ('half' as const) : ('hollow' as const), disabled: isNew && l !== locale };
    }),
  );

  // The inspector sits beside the form when there is room (the window for a full page, the pane when embedded) and
  // is a slide-over otherwise.
  let root = $state<HTMLElement>();
  let inline = $state(true);
  let panelOpen = $state(false);
  $effect(() => {
    if (!embedded) {
      const mq = matchMedia('(min-width: 1280px)');
      const set = () => (inline = mq.matches);
      set();
      mq.addEventListener('change', set);
      return () => mq.removeEventListener('change', set);
    }
    if (!root || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(([e]) => (inline = (e?.contentRect.width ?? 0) >= 1040));
    ro.observe(root);
    return () => ro.disconnect();
  });
  $effect(() => {
    if (inline) panelOpen = false;
    else if (previewOpen) panelOpen = true;
  });
  $effect(() => {
    if (!previewOpen && !inline) panelOpen = false;
  });

  // The preview pane's width: remembered, never narrower than 360 px nor wider than 80% of the editor.
  const widthKey = 'h.preview.width';
  let rootW = $state(1200);
  let paneW = $state(Number(localStorage.getItem(widthKey)) || 0);
  const paneWidth = $derived(Math.round(Math.max(360, Math.min(paneW || rootW * 0.45, rootW * 0.8))));
  $effect(() => {
    if (!root || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(([e]) => (rootW = e?.contentRect.width ?? rootW));
    ro.observe(root);
    return () => ro.disconnect();
  });
  function resizeStart(e: PointerEvent) {
    e.preventDefault();
    const startX = e.clientX;
    const start = paneWidth;
    const move = (ev: PointerEvent) => (paneW = start + (startX - ev.clientX));
    const up = () => {
      removeEventListener('pointermove', move);
      removeEventListener('pointerup', up);
      localStorage.setItem(widthKey, String(paneWidth));
    };
    addEventListener('pointermove', move);
    addEventListener('pointerup', up);
  }
  function resizeKey(e: KeyboardEvent) {
    if (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight') return;
    e.preventDefault();
    paneW = paneWidth + (e.key === 'ArrowLeft' ? 24 : -24);
    localStorage.setItem(widthKey, String(paneWidth));
  }

  // The unsaved-changes bar shows while the form is dirty and the page header has scrolled out of sight.
  let headEl = $state<HTMLElement>();
  let headGone = $state(false);
  $effect(() => {
    if (!headEl || typeof IntersectionObserver === 'undefined') return;
    const io = new IntersectionObserver(([e]) => (headGone = !!e && !e.isIntersecting));
    io.observe(headEl);
    return () => io.disconnect();
  });
  function discardEdits() {
    if (!confirm(t('editor.discardEdits'))) return;
    form.reset(structuredClone(saved));
  }
  async function copyId() {
    if (data && (await copyText(data.id))) toast.success(t('editor.idCopied'));
  }
  const isLive = $derived(!!data && (data.status === 'published' || data.status === 'changed'));
  const canUnpublish = $derived(!isNew && mode === 'item' && isLive && (data?.actions?.publish ?? true));
  const canSchedule = $derived(!isNew && mode === 'item' && (data?.actions?.publish ?? true) && session.can('release.manage'));
  const overflow = $derived<MenuItem[]>([
    ...(canDiscard ? [{ label: t('collections.discard'), icon: 'refresh', onselect: () => dialogs?.discard() }] : []),
    ...(data ? [{ label: t('editor.copyId'), icon: 'copy', onselect: () => void copyId() }] : []),
    ...(canUnpublish ? [{ label: t('release.unpublish'), icon: 'eye', separator: true, onselect: () => dialogs?.unpublish() }] : []),
    ...(canUnpublish && canSchedule ? [{ label: t('schedule.unpublishMenu'), icon: 'clock', onselect: () => void withSaved(() => dialogs?.schedule('unpublish')) }] : []),
    ...(canDelete ? [{ label: t('trash.move'), icon: 'trash', danger: true, separator: true, onselect: () => dialogs?.remove() }] : []),
  ]);
  const nothingToPublish = $derived(!canPublish && !form.dirty);
  // A release or schedule would be refused by the review gate as well: off with the same reason.
  const menuOff = $derived(nothingToPublish || !!reviewBlock);
  const menuHint = $derived(reviewBlock ? reviewReason : nothingToPublish ? t('release.nothingToPublish') : undefined);
  const publishMenu = $derived<MenuItem[]>([
    ...(bypassItem && !isNew && (data?.actions?.publish ?? true) ? [{ label: t('review.bypass'), icon: 'send', disabled: nothingToPublish, hint: nothingToPublish ? t('release.nothingToPublish') : undefined, onselect: () => (bypassOpen = true) }] : []),
    ...(canSchedule
      ? [
          { label: t('schedule.publishMenu'), icon: 'clock', disabled: menuOff, hint: menuHint, onselect: () => void withSaved(() => dialogs?.schedule('publish')) },
          { label: t('releases.addMenu'), icon: 'send', disabled: menuOff, hint: menuHint, onselect: () => void withSaved(() => dialogs?.addToRelease()) },
        ]
      : []),
  ]);
  const saveLabel = $derived(!isNew && !form.dirty && !form.pending ? t('editor.saved') : t('common.save'));
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
    <div class="h-item__grid h-item__grid--skeleton">
      <Skeleton shape="block" height="22rem" />
      <Skeleton shape="block" height="16rem" />
    </div>
  </div>
{:else}
  <div class="h-page h-item" class:h-item--embedded={embedded} class:h-item--inline={inline} bind:this={root}>
    <Slot at="editor.before" props={slotProps} />
    <div bind:this={headEl}>
      {#if embedded}
        <header class="h-item__bar">
          <div class="h-item__bar-main">
            <div class="h-item__bar-title">
              <h2>{title}</h2>
              {@render badge()}
            </div>
            <p class="h-pagehead__meta">{@render meta()}</p>
          </div>
          <div class="h-pagehead__actions">
            {@render actions()}
            {#if overflow.length}<Menu label={t('common.more')} items={overflow} />{/if}
            {@render primary()}
          </div>
        </header>
      {:else}
        <PageHeader {title} breadcrumbs={crumbs} {badge} {meta} {actions} {overflow} {primary} />
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

    <div class="h-item__grid" class:h-item__grid--preview={previewOpen} style:--h-preview-w="{paneWidth}px">
      <div class="h-item__main">
        {#if data && locale !== data.mainLocale && hasTranslatable && canEdit}
          <div class="h-item__translate">
            <span class="h-muted">{t('editor.translating', { locale })}</span>
            <Button size="sm" icon="copy" loading={copying} onclick={() => void copyFromMain()}>{t('collections.copyFrom', { locale: data.mainLocale })}</Button>
          </div>
        {/if}
        <form
          class="h-item__form"
          novalidate
          aria-label={t('collections.formLabel', { type: type.label })}
          onsubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          {#each groups as g (g.title)}
            <Card title={g.title} class="h-item__card">
              <div class="h-fieldcard">
                <div class="h-fieldgrid">
                  <Fields fields={g.fields} bind:doc={form.values.fields} />
                </div>
              </div>
            </Card>
          {/each}
          {#if type.fields.length === 0}<p class="h-muted">{t('collections.noFields')}</p>{/if}
        </form>
      </div>
      {#if panelOpen && !inline}<button type="button" class="h-item__scrim" tabindex="-1" aria-label={t('editor.inspectorClose')} onclick={() => (panelOpen = false)}></button>{/if}
      <aside
        class="h-item__side"
        class:h-item__side--open={panelOpen}
        class:h-item__side--preview={previewOpen}
        aria-label={previewOpen ? t('preview.tabs') : t('collections.details')}
      >
        {#if previewOpen}
          <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
          <div
            class="h-item__resize"
            role="separator"
            aria-orientation="vertical"
            aria-label={t('preview.resize')}
            aria-valuemin={360}
            aria-valuenow={paneWidth}
            tabindex="0"
            onpointerdown={resizeStart}
            onkeydown={resizeKey}
          ></div>
        {/if}
        {#if !inline}
          <div class="h-item__side-head">
            <IconButton icon="x" size="sm" label={t('editor.inspectorClose')} onclick={() => (previewOpen ? togglePreview() : (panelOpen = false))} />
          </div>
        {/if}
        {#if previewOpen}
          <div class="h-item__panetabs">
            <Segmented
              label={t('preview.tabs')}
              size="sm"
              options={[
                { value: 'preview', label: t('preview.tabPreview'), icon: 'eye' },
                { value: 'details', label: t('preview.tabDetails'), icon: 'panel-left' },
              ]}
              value={paneTab}
              onchange={(v) => (paneTab = v as 'preview' | 'details')}
            />
            <IconButton icon="x" size="sm" label={t('preview.toggleHide')} onclick={togglePreview} />
          </div>
          <div class="h-item__panebody" class:h-item__panebody--off={paneTab !== 'preview'}>
            {#if PreviewPane && data}
              <PreviewPane
                item={data.id}
                itemType={data.type}
                title={data.name || t('editor.untitled')}
                {locale}
                channels={previewChans}
                doc={previewDoc}
                flush={flushRelay}
                {relayState}
                dirty={form.dirty}
                {shares}
                onmode={(m: 'live' | 'reload' | null) => m && (paneMode = m)}
                principal={session.principalKey}
                fields={type?.fields ?? []}
                setField={(name: string, v: unknown) => ((form.values.fields as Record<string, unknown>)[name] = v)}
                readonly={!canEdit}
              />
            {:else}
              <Skeleton shape="block" height="20rem" />
            {/if}
          </div>
        {/if}
        <div class="h-item__panebody" hidden={previewOpen && paneTab !== 'details'}>
          <SidePanels {data} {item} {type} {locale} {locales} {form} {mode} onrefresh={extRefresh} />
        </div>
      </aside>
    </div>
    <Slot at="editor.after" props={slotProps} />

    {#if form.dirty && headGone}
      <div class="h-item__dirty" role="status">
        <span>{t('editor.unsavedBar')}</span>
        <Button variant="ghost" size="sm" onclick={discardEdits}>{t('editor.discardChanges')}</Button>
        <Button variant="secondary" size="sm" aria-label={t('editor.saveChanges')} loading={form.pending} onclick={() => void save()}>{t('common.save')}</Button>
      </div>
    {/if}
  </div>

  <ActionDialogs
    bind:this={dialogs}
    {data}
    {type}
    {locale}
    {published}
    {cc}
    onconflict={openConflict}
    onchanged={() => (reloadTick++, relTick++)}
    onscheduled={() => relTick++}
    onswitch={(l) => router.setQuery({ locale: l })}
    onerrors={(f) => {
      form.setErrors(f);
      form.formError = t('form.invalid');
    }}
  />
{/if}

{#snippet publishButton()}
  <Button
    variant="primary"
    disabled={(!canPublish && !form.dirty) || !!reviewBlock}
    aria-describedby={reviewReason ? 'h-publish-reason' : undefined}
    onclick={() => void openPublish()}>{t('collections.publish')}</Button>
  {#if reviewReason}<span id="h-publish-reason" class="h-sr">{reviewReason}</span>{/if}
{/snippet}

{#snippet badge()}
  <StatusBadge status={data ? data.status : 'draft'} />
  {#if reviewRequired && reviewState !== 'none'}
    <span class="h-item__review" title={t('review.chipTitle', { state: t(`review.state.${reviewState}`) })}><Badge tone={stateTone(reviewState)} dot>{t(`review.state.${reviewState}`)}</Badge></span>
  {/if}
  {#if scheduled}
    <a class="h-item__sched" use:link href={href(`/releases/${scheduled.id}`)} title={scheduled.name}>
      <Badge tone="info"><Icon name="clock" size={12} /> {t('release.scheduledBadge', { date: formatInZone(scheduled.scheduledAt) })}</Badge>
    </a>
  {/if}
{/snippet}

{#snippet meta()}
  {type?.label}{#if data}{' · '}{t('editor.meta.updated')}{' '}<Time at={data.updatedAt} />{' · '}<span class="h-num">{t('editor.meta.version', { n: data.version || 0 })}</span>{/if}{#if form.dirty}{' · '}<span role="status">{t('collections.unsavedBadge')}</span>{/if}
{/snippet}

{#snippet actions()}
  {#if locales.length > 5}
    <Select
      aria-label={t('collections.languages')}
      value={locale}
      options={localeOptions.map((o) => ({ value: o.value, label: o.label, disabled: o.disabled }))}
      onchange={(e) => {
        const el = e.currentTarget as HTMLSelectElement;
        if (!switchLocale(el.value)) el.value = locale;
      }}
    />
  {:else if locales.length > 1}
    <Segmented label={t('collections.languages')} options={localeOptions} bind:value={() => locale, (l) => void switchLocale(l)} />
  {/if}
  {#if previewReason}
    <span title={previewReason}>
      <Button icon="eye" disabled>{t('collections.preview.button')}</Button>
      <span class="h-sr">{previewReason}</span>
    </span>
  {:else}
    <Button icon="eye" aria-pressed={previewOpen} title={t('preview.toggleTitle')} onclick={togglePreview}>{t('collections.preview.button')}</Button>
  {/if}
  <Button
    variant={isNew ? 'primary' : 'secondary'}
    icon={saveLabel === t('editor.saved') ? 'check' : undefined}
    loading={form.pending}
    disabled={!canEdit || (!form.dirty && !isNew)}
    onclick={() => void save()}>{saveLabel}</Button>
  {#if reviewRequired && data && !form.pending}
    <ReviewActions id={data.id} {review} before={async () => !form.dirty || (await save())} />
  {/if}
  {#each extActions as a, i (a.tag + i)}
    <span class="h-item__ext" data-action={a.tag} title={a.title}>
      <Extension tag={a.tag} props={{ item: data, type: type?.name, locale }} onEvents={extEvents} />
    </span>
  {/each}
  <span class="h-item__toggle"><IconButton icon="panel-left" variant="secondary" label={t('editor.inspector')} aria-expanded={panelOpen} onclick={() => (panelOpen = !panelOpen)} /></span>
{/snippet}

{#snippet primary()}
  {#if !isNew}
    {#if publishMenu.length}
      <span class="h-split">
        {@render publishButton()}
        <Menu label={t('release.publishOptions')} items={publishMenu} triggerClass="h-button h-button--primary h-button--md h-split__more">
          {#snippet trigger()}<Icon name="chevron-down" size={16} /><span class="h-sr">{t('release.publishOptions')}</span>{/snippet}
        </Menu>
      </span>
    {:else}
      {@render publishButton()}
    {/if}
  {/if}
{/snippet}

<Dialog bind:open={bypassOpen} title={t('review.bypassTitle')} size="sm">
  <p>{t('review.bypassText', { name: data?.name || t('editor.untitled') })}</p>
  {#snippet footer()}
    <Button disabled={bypassBusy} onclick={() => (bypassOpen = false)}>{t('common.cancel')}</Button>
    <Button
      variant="danger"
      loading={bypassBusy}
      onclick={() => void publishBypass()}>{t('review.bypass')}</Button>
  {/snippet}
</Dialog>

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
  .h-item__bar {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: var(--h-space-3) var(--h-space-4);
  }
  .h-item__bar-main {
    min-width: 0;
  }
  .h-item__bar-title {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--h-space-3);
  }
  .h-item__bar-title h2 {
    font-size: var(--h-text-2xl);
    font-weight: 650;
    letter-spacing: -0.02em;
    line-height: 1.2;
    overflow-wrap: anywhere;
  }
  .h-item__ext {
    display: inline-flex;
  }
  .h-item__grid {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--h-space-4);
    align-items: start;
  }
  .h-item__grid--skeleton {
    grid-template-columns: minmax(0, 1fr) 22.5rem;
  }
  .h-item__main {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-3);
    min-width: 0;
  }
  .h-item__form {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-4);
  }
  .h-item__translate {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--h-space-3);
    padding: var(--h-space-2) var(--h-space-3);
    border-radius: var(--h-radius-md);
    background: var(--h-surface-2);
    font-size: var(--h-text-sm);
  }
  .h-item__unrouted ul {
    margin: 0;
    padding-left: var(--h-space-4);
  }

  /* Inspector: a column beside the form when there is room, a slide-over otherwise. */
  .h-item--inline .h-item__grid {
    grid-template-columns: minmax(0, 1fr) 22.5rem;
    gap: var(--h-space-5);
  }
  @media (min-width: 1680px) {
    .h-item:not(.h-item--embedded) .h-item__grid {
      grid-template-columns: minmax(0, 1fr) 25rem;
    }
  }
  .h-item__side {
    min-width: 0;
    padding: var(--h-space-4);
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius-lg);
    background: var(--h-surface);
  }
  .h-item--inline .h-item__side {
    position: sticky;
    top: calc(var(--h-topbar-height) + var(--h-space-3));
    max-height: calc(100vh - var(--h-topbar-height) - var(--h-space-5));
    overflow-y: auto;
    scrollbar-width: thin;
  }
  .h-item--embedded.h-item--inline .h-item__side {
    top: 0;
    max-height: calc(100vh - var(--h-topbar-height) - var(--h-space-7));
  }
  .h-item:not(.h-item--inline) .h-item__side {
    position: fixed;
    inset: var(--h-topbar-height) 0 0 auto;
    z-index: calc(var(--h-z-nav) + 1);
    width: min(25rem, 100vw);
    overflow-y: auto;
    border-width: 0 0 0 1px;
    border-radius: 0;
    box-shadow: var(--h-shadow-dialog);
    visibility: hidden;
    transform: translateX(100%);
    transition:
      transform var(--h-dur),
      visibility var(--h-dur);
  }
  .h-item:not(.h-item--inline) .h-item__side--open {
    visibility: visible;
    transform: none;
  }
  .h-item--inline .h-item__grid--preview {
    grid-template-columns: minmax(0, 1fr) var(--h-preview-w);
    gap: var(--h-space-3);
  }
  .h-item__side--preview {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
  }
  .h-item--inline .h-item__side--preview {
    height: calc(100vh - var(--h-topbar-height) - var(--h-space-5));
  }
  .h-item--embedded.h-item--inline .h-item__side--preview {
    height: calc(100vh - var(--h-topbar-height) - var(--h-space-7));
  }
  .h-item:not(.h-item--inline) .h-item__side--preview {
    width: min(48rem, 100vw);
    height: auto;
  }
  .h-item__panetabs {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--h-space-2);
  }
  .h-item__panebody {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }
  .h-item__panebody[hidden] {
    display: none;
  }
  /* The preview stays laid out (not display: none) while the Details tab shows, so the frame keeps rendering. */
  .h-item__panebody--off {
    position: absolute;
    inset: 0;
    visibility: hidden;
    pointer-events: none;
  }
  .h-item__resize {
    position: absolute;
    inset-block: 0;
    inset-inline-start: -10px;
    z-index: 2;
    width: 10px;
    cursor: col-resize;
    touch-action: none;
  }
  .h-item__resize::after {
    content: '';
    position: absolute;
    inset-block: 30% 30%;
    inset-inline-start: 4px;
    width: 3px;
    border-radius: 2px;
    background: var(--h-border-strong);
    opacity: 0;
    transition: opacity var(--h-dur-fast);
  }
  .h-item__resize:hover::after,
  .h-item__resize:focus-visible::after {
    opacity: 1;
  }
  :global(.h-item .h-button[aria-pressed='true']) {
    background: var(--h-surface-3);
  }
  .h-item__side-head {
    display: flex;
    justify-content: flex-end;
    margin-bottom: var(--h-space-2);
  }
  .h-item__scrim {
    position: fixed;
    inset: var(--h-topbar-height) 0 0 0;
    z-index: var(--h-z-nav);
    border: 0;
    background: var(--h-overlay);
  }
  .h-item--inline .h-item__toggle {
    display: none;
  }
  .h-item__toggle {
    display: inline-flex;
  }

  .h-item__dirty {
    position: sticky;
    bottom: var(--h-space-4);
    z-index: var(--h-z-sticky);
    display: flex;
    align-items: center;
    align-self: center;
    gap: var(--h-space-2);
    padding: var(--h-space-2) var(--h-space-2) var(--h-space-2) var(--h-space-4);
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius-pill);
    background: var(--h-surface);
    box-shadow: var(--h-shadow-dialog);
    font-size: var(--h-text-sm);
    font-weight: 500;
  }
  @media (prefers-reduced-motion: reduce) {
    .h-item:not(.h-item--inline) .h-item__side {
      transition: none;
    }
  }
</style>
