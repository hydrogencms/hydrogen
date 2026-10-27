<script lang="ts">
  import '../../lib/messages/collections';
  import '../../lib/messages/editor';
  import '../../lib/messages/collab';
  import { config } from '../../lib/config';
  import { connect, type CollabConnection } from '../../lib/collab/transport';
  import { Presence, focusPath } from '../../lib/collab/presence.svelte';
  import { COLLAB_CTX, type CollabCtx } from '../../lib/collab/context';
  import type { BindingInfo } from '../../lib/collab/bind.svelte';
  import PresenceAvatars from '../../lib/collab/PresenceAvatars.svelte';
  import PresenceBanner from '../../lib/collab/PresenceBanner.svelte';
  import PresenceLive from '../../lib/collab/PresenceLive.svelte';
  import PresenceMarks from '../../lib/collab/PresenceMarks.svelte';
  import { api, ApiError } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { channels } from '../../lib/channels.svelte';
  import { provideFields, type OverlayCtx } from '../../lib/fields/context';
  import '../../lib/messages/overlays';
  import Fields from '../../lib/fields/Fields.svelte';
  import { setContext, untrack, type Component } from 'svelte';
  import '../../lib/messages/preview';
  import { copyTranslatable, fieldsPatch, isHomePage, hydrate, initialValues, cleanDoc, jsonEqual, overlayState, overrideValue, resolvedDoc, unroutedErrors, viewValues, type Doc } from '../../lib/fields/logic';
  import { itemLocales, knownLocales, localeState, overlayNote } from '../../lib/fields/locales';
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
  // Tells the server this write is the collaboration session's own: the others get `saved`, not `reset`. `seq` is
  // the highest update this client applied (its own acknowledged ones included); the others compare it with theirs.
  const collabHeaders = (seq?: number): Record<string, string> | undefined => {
    const c = collab?.tr.client;
    return c ? { 'X-Hydrogen-Collab-Client': c, 'X-Hydrogen-Collab-Seq': String(seq ?? collabBinding?.applied() ?? 0) } : undefined;
  };
  const cc = (noMatch = false, seq?: number): Concurrency => ({
    ifMatch: noMatch ? undefined : etag || undefined,
    headers: collabHeaders(seq),
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

  // ---- collaboration (presence, soft locks, co-editing): one session per loaded item, re-joined on every load
  const presence = new Presence();
  let collab = $state.raw<{ tr: CollabConnection } | null>(null);
  let collabBinding = $state.raw<BindingInfo>();
  // The session was given up on this page (a declined reset, a chunk that did not load): the banner stays.
  let collabLost = $state(false);
  let collabGen = 0;
  let joinReloads = 0;
  let claimRestarts = 0;
  // After a reset the reload re-joins with the same client id (and keeps its header); the stream ended, nothing to leave.
  let resumeClient = '';
  // The page went into the back/forward cache with a session: it joins again when shown.
  let suspended = false;
  // After a lost seeding claim: the parts whose form edits the new session must merge.
  let restartMerge: string[] | undefined;
  let restartResume: { parts: string[]; base: Record<string, unknown> } | undefined;
  setContext<CollabCtx>(COLLAB_CTX, {
    get binding() {
      return collabBinding;
    },
  });
  // `collab` in the admin's config.json turns the feature off; absent counts as on (a failed join shows the banner).
  const collabWanted = () => (config() as { collab?: boolean }).collab !== false;
  const sessionRO = $derived(!!collabBinding?.readOnly);

  function stopCollab(opts: { discard?: boolean } = {}) {
    collabGen++;
    lateSaved = undefined;
    const b = collabBinding;
    const c = collab;
    const keep = !!resumeClient;
    collabBinding = undefined;
    collab = null;
    // Unsent edits go out first; the leave waits for them.
    b?.destroy(opts);
    c?.tr.leave(keep ? { keep: true } : undefined);
    presence.detach();
  }
  async function startCollab(d: ItemData) {
    stopCollab();
    const resume = resumeClient;
    resumeClient = '';
    const merge = restartMerge;
    const resumeFrom = restartResume;
    restartMerge = restartResume = undefined;
    collabLost = false;
    const g = collabGen;
    const ty = types.get(d.type);
    if (mode === 'new' || !d.id || !ty || !collabWanted()) return;
    // The Yjs chunk loads before the join: nothing the session sends meanwhile has nobody to receive it.
    const chunk = import('../../lib/collab/bind.svelte');
    const tr = connect(d.id, undefined, resume);
    collab = { tr };
    const loc = d.locale || locale;
    presence.attach(tr, () => locale);
    tr.onSaved((s) => void onCollabSaved(g, tr, s));
    tr.onReset((r) => onCollabReset(g, r));
    try {
      const m = await chunk;
      if (g !== collabGen) return;
      const join = await tr.join(loc, canEdit);
      if (g !== collabGen) return;
      // The draft changed between our read and the join: seeding from it would be stale.
      if (join.init.length && join.etag && etag && join.etag !== etag && joinReloads < 1) {
        joinReloads++;
        stopCollab();
        reloadTick++;
        return;
      }
      joinReloads = 0;
      collabBinding = m.bindForm({ form, transport: tr, join, type: ty, locale: loc, draft: { ...d, fields: viewValues(ty.fields, d) }, sparse: !!d.inherits, merge, resume: resumeFrom, onFatal: (r) => onCollabFatal(g, r) });
    } catch {
      // The transport reports `unavailable` (the banner) and retries what may pass; a chunk that failed to load
      // never reports anything, so say so here.
      if (g === collabGen && tr.state !== 'unavailable') collabLost = true;
    }
  }
  const sameValues = (a: Values, b: Values) => jsonEqual(JSON.parse(JSON.stringify(a)), JSON.parse(JSON.stringify(b)));
  async function onCollabSaved(g: number, tr: CollabConnection, s: { etag: string; client: string; name: string; seq: number }) {
    if (g !== collabGen || s.client === tr.client) return;
    etag = s.etag;
    gotTag = true;
    toast.info(t('collab.saved', { name: s.name || t('collections.conflict.someone') }));
    if (!target || !type) return;
    const fresh = await itemApi.read(target, locale, undefined, cc()).catch(() => null);
    if (!fresh || g !== collabGen) return;
    data = fresh;
    const fv: Values = { fields: viewValues(type.fields, fresh), key: fresh.key ?? '', alwaysAvailable: fresh.alwaysAvailable };
    // What the draft holds now is what a later save must be measured against.
    saved = structuredClone(fv);
    // Clean only when nothing local is missing from the save, and the saved draft is what the form shows.
    lateSaved = { fv, seq: s.seq };
    settleSaved();
  }
  // A save of another participant that did not make the form clean at once: an unsent or unacknowledged edit of
  // ours, or an update of theirs still on its way. It is looked at again whenever the binding or the form changes.
  let lateSaved = $state.raw<{ fv: Values; seq: number } | undefined>();
  function settleSaved() {
    const r = lateSaved;
    const b = collabBinding;
    if (!r || !b) return;
    // The save lacks an update we hold: this form stays dirty (with the new ETag) until the next save.
    if (b.applied() > r.seq) {
      lateSaved = undefined;
      return;
    }
    if (b.unsynced() || form.pending) return;
    if (sameValues(r.fv, $state.snapshot(form.values) as Values)) {
      lateSaved = undefined;
      form.markSaved();
    }
  }
  $effect(() => {
    // Tracked: the binding's state (reactive) and the form values, while a save waits to be settled.
    if (!lateSaved || !collabBinding) return;
    void collabBinding.unsynced();
    void collabBinding.applied();
    void $state.snapshot(form.values);
    untrack(settleSaved);
  });
  function onCollabReset(g: number, reason: string) {
    if (g !== collabGen) return;
    if (reason !== 'trashed' && collabBinding?.unsynced() && !confirm(t('collab.resetConfirm'))) {
      stopCollab();
      collabLost = true;
      return;
    }
    toast.info(t(`collab.reset.${reason}`));
    resumeClient = collab?.tr.client ?? '';
    reloadTick++;
  }
  // The session cannot go on as it is.
  function onCollabFatal(g: number, reason: 'claim_lost' | 'poison' | 'reset-failed') {
    if (g !== collabGen) return;
    if (reason === 'poison') return; // the server's `reset` follows and reloads
    if (reason === 'claim_lost' && data && claimRestarts < 3) {
      // The part this client seeded went to someone else: join again from the draft; edits made meanwhile stay.
      claimRestarts++;
      resumeClient = collab?.tr.client ?? '';
      const d = data;
      restartMerge = collabBinding?.lostParts();
      restartResume = collabBinding?.ackedValues();
      stopCollab({ discard: true });
      void startCollab(d);
      return;
    }
    if (collabBinding?.unsynced() && !confirm(t('collab.resetConfirm'))) {
      stopCollab();
      collabLost = true;
      return;
    }
    resumeClient = '';
    reloadTick++;
  }
  function onPageShow(e: PageTransitionEvent) {
    if (e.persisted && suspended && data) {
      suspended = false;
      resumeClient = '';
      void startCollab(data);
    }
  }
  function onPageHide(e: PageTransitionEvent) {
    // The transport leaves by itself (and flushes); a restored page joins again in onPageShow.
    if (e.persisted && collab) suspended = true;
  }
  $effect(() => () => stopCollab());
  let formEl = $state<HTMLElement>();

  const form = new Form<Values>({ fields: {}, key: '', alwaysAvailable: false }, { prefix: 'fields.' });
  form.guard();

  // The draft view lists draft-only locales too; the published ones decide the state shown per locale.
  let published = $state<string[]>([]);
  // Stored plus derived (published through inheritance) locales.
  let available = $state<string[]>([]);
  $effect(() => {
    const d = data;
    // Nothing is published for a never-published or unpublished item.
    if (!d || d.status === 'draft' || d.status === 'unpublished') {
      published = [];
      available = [];
      return;
    }
    if (!d.draftParts.some((p) => p !== 'shared')) {
      published = d.locales;
      available = d.availableLocales ?? d.locales;
      return;
    }
    const ctl = new AbortController();
    api
      .get('/items/{id}', { params: { id: d.id }, query: { locale: d.mainLocale } as never, signal: ctl.signal })
      .then((v) => {
        published = v.locales;
        available = (v as { availableLocales?: string[] }).availableLocales ?? v.locales;
      })
      .catch(() => {
        published = d.locales.filter((l) => !d.draftParts.includes(l));
        available = published;
      });
    return () => ctl.abort();
  });
  const item = $derived(data ? { ...data, locales: published, availableLocales: available } : null);

  const urlLocale = $derived(router.query.get('locale') ?? '');
  const type = $derived(types.get(mode === 'item' ? (data?.type ?? '') : typeName));
  const isNew = $derived(mode === 'new' || data === null);
  const locale = $derived(data?.locale ?? (urlLocale || knownLocales()[0] || ''));
  const target = $derived<Target | null>(mode === 'item' ? { kind: 'item', id } : mode === 'singleton' ? { kind: 'singleton', type: typeName } : null);
  const canEdit = $derived(isNew ? session.can('content.create', { type: type?.name }) : (data?.actions?.edit ?? true));
  const locales = $derived(item ? itemLocales(knownLocales(), item) : [locale].filter(Boolean));
  const hasTranslatable = $derived(!!type?.fields.some((f) => f.translatable));

  // ---- locale inheritance: the form holds only the overridden translatable fields
  // The parent's resolved draft view (its values are what an inherited field shows and what Reset falls back to).
  let parentView = $state.raw<{ locale: string; fields: Doc; inheritedFrom?: Record<string, string> } | null>(null);
  $effect(() => {
    const d = data;
    const tgt = target;
    const ty = type;
    if (!d?.inherits || !tgt || !ty) {
      parentView = null;
      return;
    }
    const ctl = new AbortController();
    const parent = d.inherits;
    itemApi
      .read(tgt, parent, ctl.signal)
      .then((v) => (parentView = { locale: parent, fields: hydrate(ty.fields, v.fields), inheritedFrom: v.inheritedFrom }))
      .catch(() => undefined);
    return () => ctl.abort();
  });
  const inheritedValue = (name: string): unknown => {
    const pv = parentView && parentView.locale === data?.inherits ? parentView : null;
    return $state.snapshot(pv ? pv.fields[name] : data?.fields[name]);
  };
  const overlay = $derived<OverlayCtx | undefined>(
    data?.inherits
      ? {
          parent: data.inherits,
          state: (name) => overlayState(form.values.fields, name),
          source(name) {
            const pv = parentView && parentView.locale === data?.inherits ? parentView : null;
            return (pv ? pv.inheritedFrom?.[name] : data?.inheritedFrom?.[name]) ?? data?.inherits ?? '';
          },
          value: inheritedValue,
          resolved: () => resolvedDoc(type?.fields ?? [], form.values.fields, parentView?.fields ?? data?.fields),
          override(name) {
            const f = type?.fields.find((x) => x.name === name);
            if (!f || name in form.values.fields) return;
            form.values.fields[name] = overrideValue(f, inheritedValue(name));
            form.clearError(name);
          },
          reset(name) {
            delete form.values.fields[name];
            form.clearError(name);
          },
        }
      : undefined,
  );

  // The home page (route `/`) shows a hint under its slug; its paths are the lightest data that tells.
  let homePage = $state(false);
  const homeId = $derived(data?.id);
  const homeVersion = $derived(data?.version);
  $effect(() => {
    const id = homeId;
    void homeVersion;
    if (!id || !config().pages || !session.can('pages.read')) {
      homePage = false;
      return;
    }
    const ctl = new AbortController();
    api
      .get('/pages/items/{id}/paths', { params: { id }, signal: ctl.signal })
      .then((r) => (homePage = isHomePage(r.items as { path: string; state: string }[])))
      .catch(() => {
        if (!ctl.signal.aborted) homePage = false;
      });
    return () => ctl.abort();
  });

  provideFields({
    isHome: () => homePage,
    get overlay() {
      return overlay;
    },
    error: (p) => form.error(p),
    clearError: (p) => form.clearError(p),
    errors: () => form.errors,
    get readonly() {
      return !canEdit || sessionRO;
    },
    loading(name) {
      const b = collabBinding;
      const f = type?.fields.find((x) => x.name === name);
      return !!b && !!f && b.pending().includes(f.translatable ? `l:${data?.locale || locale}` : 'shared');
    },
    collab: () => !!collabBinding,
    get locale() {
      return locale;
    },
    get type() {
      return type?.name;
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
    const v: Values = { fields: viewValues(t0?.fields ?? [], d), key: d.key ?? '', alwaysAvailable: d.alwaysAvailable };
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
      // Untracked: the options read the etag and the collab connection, which loading and joining change.
      .read(tgt, loc, ctl.signal, untrack(() => cc()))
      .then((d) => {
        apply(d);
        loading = false;
        void startCollab(d);
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        loading = false;
        stopCollab();
        resumeClient = '';
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
    let sent: Values | undefined;
    gotTag = false;
    // Read before the snapshot is taken: a claim of less than the snapshot holds is the safe side for the others.
    const seq = collabBinding?.applied();
    const ok = await form.submit(async (v) => {
      sent = structuredClone(v);
      const body: Record<string, unknown> = {};
      if (locale) body.locale = locale;
      if (data) {
        const patch = fieldsPatch(type.fields, saved.fields, v.fields, !!data.inherits);
        if (v.key !== saved.key) body.key = v.key;
        if (v.alwaysAvailable !== saved.alwaysAvailable) body.alwaysAvailable = v.alwaysAvailable;
        if (!Object.keys(patch).length && !('key' in body) && !('alwaysAvailable' in body)) {
          // Only normalisation differed: nothing to send, the form is clean.
          saved = structuredClone(v);
          return;
        }
        body.fields = patch;
        try {
          result = await itemApi.update(target!, body, cc(force, seq));
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
      // With a live binding the form already holds the merged document: replacing its values would fight the
      // session (and the others' carets), so only the item's metadata and the saved baseline move.
      if (collabBinding && !created) {
        data = result;
        // What was sent is what was saved; edits and remote changes that came in during the request stay dirty.
        saved = structuredClone(sent!);
        form.markSavedAs(sent!);
      } else apply(result);
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
  const workingFields = $derived(type && (previewOpen || relayWanted) ? cleanDoc(type.fields, $state.snapshot(overlay ? overlay.resolved() : form.values.fields) as Doc) : null);
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

  const unrouted = $derived(type ? unroutedErrors(type.fields, form.errors, overlay ? overlay.resolved() : form.values.fields) : {});
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

  // Parents of the inheriting locales seen so far (the view of a locale says which one it inherits from).
  let parents = $state<Record<string, string>>({});
  $effect(() => {
    const d = data;
    if (d?.inherits && parents[d.locale] !== d.inherits) parents[d.locale] = d.inherits;
  });
  const localeOptions = $derived(
    locales.map((l) => {
      const s = item ? localeState(l, item) : l === locale ? 'draft' : 'missing';
      const parent = l === locale ? data?.inherits : parents[l];
      const note = item && (parent || item.availableLocales?.includes(l)) ? overlayNote(l, item) : '';
      const state = note ? (note === 'derived' ? (parent ? t('overlay.state.derived', { locale: parent }) : t('overlay.state.derivedPlain')) : t(`overlay.state.${note}`)) : '';
      const dot = s === 'published' || note === 'derived' ? ('filled' as const) : s === 'draft' ? ('half' as const) : ('hollow' as const);
      return {
        value: l,
        label: l,
        sub: parent ? t('overlay.inherits', { locale: parent }) : undefined,
        title: [parent ? t('overlay.inherits', { locale: parent }) : '', state].filter(Boolean).join(', ') || undefined,
        dot,
        disabled: isNew && l !== locale,
      };
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

<svelte:window onpageshow={onPageShow} onpagehide={onPageHide} />

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

    <div class="h-collab-status" role="status" aria-live="polite" aria-atomic="true">
      {#if collabLost || presence.unavailable}
        <PresenceBanner kind="unavailable" />
      {:else if collabBinding?.failed()}
        <PresenceBanner kind="failed" />
      {:else if sessionRO && canEdit}
        <PresenceBanner kind="readOnly" />
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
        {#if data?.inherits}
          <div class="h-item__translate"><span class="h-muted">{t('overlay.banner', { locale, parent: data.inherits })}</span></div>
        {:else if data && locale !== data.mainLocale && hasTranslatable && canEdit}
          <div class="h-item__translate">
            <span class="h-muted">{t('editor.translating', { locale })}</span>
            <Button size="sm" icon="copy" loading={copying} onclick={() => void copyFromMain()}>{t('collections.copyFrom', { locale: data.mainLocale })}</Button>
          </div>
        {/if}
        <form
          class="h-item__form"
          data-collab={collabBinding ? 'live' : presence.state}
          bind:this={formEl}
          onfocusin={(e) => presence.focus(focusPath(e.target))}
          onfocusout={(e) => !formEl?.contains(e.relatedTarget as Node | null) && presence.focus(undefined)}
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
        <PresenceMarks root={formEl} others={presence.others} />
        <PresenceLive root={formEl} others={presence.others} />
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
                readonly={!canEdit || sessionRO}
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
  <PresenceAvatars others={presence.others} />
  {#if locales.length > 5}
    <Select
      aria-label={t('collections.languages')}
      bind:value={() => locale, (l) => void switchLocale(l)}
      options={localeOptions.map((o) => ({ value: o.value, label: o.sub ? `${o.label} (${o.sub})` : o.label, disabled: o.disabled }))}
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
