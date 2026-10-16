<script lang="ts">
  import { ApiError } from '../../lib/api/client';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import type { CompiledType } from '../../lib/types.svelte';
  import { types } from '../../lib/types.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Checkbox from '../../lib/ui/Checkbox.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { errorMessage } from '../../lib/errors';
  import '../../lib/messages/releases';
  import { isConflict, itemApi, type Concurrency, type ItemData, type Referrer, type Target } from './model';
  import { releaseApi } from '../release/api';
  import { scheduleName } from '../release/logic';
  import AddToReleaseDialog from '../release/AddToReleaseDialog.svelte';
  import ScheduleDialog from '../release/ScheduleDialog.svelte';

  let {
    data,
    type,
    locale,
    published,
    cc,
    onconflict,
    onchanged,
    onswitch,
    onerrors,
    onscheduled,
  }: {
    data: ItemData | null;
    type: CompiledType;
    locale: string;
    /** Locales with published content (the draft view's `locales` also lists draft-only ones). */
    published: string[];
    /** Concurrency options for a write (If-Match from the editor's ETag; `true` omits it). */
    cc: (noMatch?: boolean) => Concurrency;
    /** A 412: the editor shows the conflict dialog and may call `retry` (without If-Match). */
    onconflict: (e: ApiError, retry: () => void) => void;
    onchanged: () => void;
    onswitch: (locale: string) => void;
    onerrors: (fields: ApiError['fields']) => void;
    /** A schedule or release membership changed (the editor reloads its badge). */
    onscheduled?: () => void;
  } = $props();

  const target = $derived<Target | null>(data ? (type.singleton ? { kind: 'singleton', type: type.name } : { kind: 'item', id: data.id }) : null);
  const parts = $derived(data?.draftParts ?? []);
  const localeParts = $derived(parts.filter((p) => p !== 'shared'));

  // ---- publish
  let pubOpen = $state(false);
  let pubSel = $state(new Set<string>());
  let pubBusy = $state(false);
  let pubError = $state('');
  let pubConflict = $state<string[] | null>(null);
  let pubFix = $state('');

  export function publish() {
    if (!data) return;
    pubSel = new Set(localeParts);
    pubError = '';
    pubConflict = null;
    pubFix = '';
    pubOpen = true;
  }
  const needsMain = $derived(!!data && data.status === 'draft' && localeParts.length > 0 && !pubSel.has(data.mainLocale));
  const pubBlocked = $derived(pubBusy || needsMain || (localeParts.length > 0 && pubSel.size === 0));

  async function doPublish(force = false, noMatch = false) {
    if (!target) return;
    pubBusy = true;
    pubError = '';
    pubFix = '';
    try {
      const body: Record<string, unknown> = {};
      if (localeParts.length) body.locales = [...pubSel].sort();
      if (force) body.force = true;
      await itemApi.publish(target, body, cc(noMatch));
      pubOpen = false;
      toast.success(t('collections.published'));
      onchanged();
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500 || e.status === 0) throw e;
      if (isConflict(e)) {
        pubOpen = false;
        onconflict(e, () => void doPublish(force, true));
      } else if (e.code === 'conflict') {
        pubConflict = (e.body?.parts as string[] | undefined) ?? [];
        pubError = t('collections.publishConflict');
      } else if (e.code === 'invalid') {
        const bad = e.body?.locale as string | undefined;
        pubError = t('collections.publishInvalid', { locale: bad ?? locale });
        if (!bad || bad === locale) {
          onerrors(e.fields);
          pubOpen = false;
        } else pubFix = bad;
      } else pubError = errorMessage(e);
    } finally {
      pubBusy = false;
    }
  }
  function toggle(set: Set<string>, key: string, on: boolean) {
    const n = new Set(set);
    if (on) n.add(key);
    else n.delete(key);
    return n;
  }

  // ---- discard
  let disOpen = $state(false);
  let disSel = $state(new Set<string>());
  let disBusy = $state(false);
  export function discard() {
    disSel = new Set(parts);
    disOpen = true;
  }
  async function doDiscard(noMatch = false) {
    if (!data) return;
    disBusy = true;
    try {
      await itemApi.discard(data.id, disSel.size === parts.length ? undefined : [...disSel], cc(noMatch));
      disOpen = false;
      toast.success(t('collections.discarded'));
      if (data.status === 'draft' && disSel.size === parts.length) {
        // A never-published item has nothing left once its drafts are gone.
        void router.navigate(type.singleton ? '/' : `/collections/${type.name}`, { force: true });
      } else onchanged();
    } catch (e) {
      if (isConflict(e)) {
        disOpen = false;
        onconflict(e as ApiError, () => void doDiscard(true));
      } else if (e instanceof ApiError && e.status < 500 && e.status > 0) toast.error(e.status === 404 ? t('collections.itemNotFound') : e.message);
      else throw e;
    } finally {
      disBusy = false;
    }
  }

  // ---- delete
  let delOpen = $state(false);
  let delBusy = $state(false);
  let blockers = $state<Referrer[] | null>(null);
  let hidden = $state(0);
  let delError = $state('');
  export function remove() {
    blockers = null;
    hidden = 0;
    delError = '';
    delOpen = true;
  }
  async function doDelete(noMatch = false) {
    if (!data) return;
    delBusy = true;
    delError = '';
    try {
      await itemApi.remove(data.id, cc(noMatch));
      delOpen = false;
      toast.success(t('trash.moved'));
      void router.navigate(`/collections/${type.name}`, { force: true });
    } catch (e) {
      if (isConflict(e)) {
        delOpen = false;
        onconflict(e as ApiError, () => void doDelete(true));
      } else if (e instanceof ApiError && e.code === 'referenced') {
        blockers = (e.body?.referrers as Referrer[] | undefined) ?? [];
        hidden = (e.body?.hidden as number | undefined) ?? 0;
      } else if (e instanceof ApiError && e.status < 500 && e.status > 0) {
        delError = errorMessage(e);
      } else throw e;
    } finally {
      delBusy = false;
    }
  }

  // ---- unpublish
  let unpOpen = $state(false);
  let unpBusy = $state(false);
  let unpError = $state('');
  export function unpublish() {
    unpError = '';
    unpOpen = true;
  }
  async function doUnpublish(noMatch = false) {
    if (!data || !target) return;
    unpBusy = true;
    unpError = '';
    try {
      await itemApi.unpublish(target, cc(noMatch));
      unpOpen = false;
      toast.success(t('release.unpublished'));
      onchanged();
    } catch (e) {
      if (isConflict(e)) {
        unpOpen = false;
        onconflict(e as ApiError, () => void doUnpublish(true));
      } else if (e instanceof ApiError && e.status < 500 && e.status > 0) unpError = errorMessage(e);
      else throw e;
    } finally {
      unpBusy = false;
    }
  }

  // ---- schedule (a one-item release) and add to release
  let schedAction = $state<'publish' | 'unpublish'>('publish');
  let schedOpen = $state(false);
  export function schedule(action: 'publish' | 'unpublish') {
    schedAction = action;
    schedOpen = true;
  }
  async function doSchedule(at: string) {
    if (!data) return;
    const rel = await releaseApi.create({ name: scheduleName(data.name || data.id, schedAction, at), kind: 'item' });
    try {
      await releaseApi.putItem(rel.id, data.id, { action: schedAction, ...(schedAction === 'publish' && localeParts.length ? { locales: localeParts } : {}) });
      await releaseApi.schedule(rel.id, at);
    } catch (e) {
      await releaseApi.remove(rel.id).catch(() => undefined);
      throw e;
    }
    toast.success(t(schedAction === 'publish' ? 'schedule.donePublish' : 'schedule.doneUnpublish'));
    onscheduled?.();
  }

  let addOpen = $state(false);
  export function addToRelease() {
    addOpen = true;
  }
</script>

<Dialog bind:open={pubOpen} title={t('collections.publishTitle')} dismissible={!pubBusy}>
  {#if data}
    <p>{t('collections.publishText', { name: data.name })}</p>
    {#if localeParts.length}
      <fieldset class="h-adlg__parts">
        <legend>{t('collections.publishLocales')}</legend>
        {#each localeParts as p (p)}
          <div class="h-row">
            <Checkbox checked={pubSel.has(p)} onchange={(e) => (pubSel = toggle(pubSel, p, (e.currentTarget as HTMLInputElement).checked))}>
              {p}
              {#if p === data.mainLocale}<span class="h-muted">({t('collections.mainLocale')})</span>{/if}
            </Checkbox>
            <Badge tone={published.includes(p) ? 'warning' : 'info'}>
              {published.includes(p) ? t('collections.pendingChanges') : t('collections.notPublished')}
            </Badge>
          </div>
        {/each}
      </fieldset>
    {/if}
    {#if parts.includes('shared')}<p class="h-muted">{t('collections.publishShared')}</p>{/if}
    {#if needsMain}<p class="h-alert" role="alert">{t('collections.publishMain', { locale: data.mainLocale })}</p>{/if}
    {#if pubError}
      <p class="h-alert" role="alert">
        {pubError}
        {#if pubConflict?.length}<strong>{pubConflict.join(', ')}</strong>{/if}
        {#if pubFix}<Button size="sm" onclick={() => ((pubOpen = false), onswitch(pubFix))}>{t('collections.switchTo', { locale: pubFix })}</Button>{/if}
      </p>
    {/if}
  {/if}
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (pubOpen = false)}>{t('common.cancel')}</Button>
    {#if pubConflict}<Button variant="danger" loading={pubBusy} onclick={() => void doPublish(true)}>{t('collections.publishAnyway')}</Button>{/if}
    <Button variant="primary" loading={pubBusy} disabled={pubBlocked} onclick={() => void doPublish()}>{t('collections.publish')}</Button>
  {/snippet}
</Dialog>

<Dialog bind:open={disOpen} title={t('collections.discardTitle')} dismissible={!disBusy}>
  <p>{t('collections.discardText')}</p>
  <fieldset class="h-adlg__parts">
    <legend>{t('collections.discardParts')}</legend>
    {#each parts as p (p)}
      <Checkbox checked={disSel.has(p)} onchange={(e) => (disSel = toggle(disSel, p, (e.currentTarget as HTMLInputElement).checked))}>
        {p === 'shared' ? t('collections.sharedPart') : p}
      </Checkbox>
    {/each}
  </fieldset>
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (disOpen = false)}>{t('common.cancel')}</Button>
    <Button variant="danger" loading={disBusy} disabled={disSel.size === 0} onclick={() => void doDiscard()}>{t('collections.discard')}</Button>
  {/snippet}
</Dialog>

<Dialog bind:open={delOpen} title={t('trash.dialogTitle')} dismissible={!delBusy}>
  {#if data}<p>{t('trash.text', { name: data.name || data.id })}</p>{/if}
  {#if delError}<p class="h-alert" role="alert">{delError}</p>{/if}
  {#if blockers}
    <div class="h-alert h-adlg__blockers" role="alert">
      <div>
        <p><strong>{t('trash.blocked', { n: blockers.length + hidden })}</strong></p>
        <ul>
          {#each blockers as b (b.id + b.field)}
            <li>
              <a use:link href={href(`/items/${b.id}`)} onclick={() => (delOpen = false)}>{b.name || b.id}</a>
              <span class="h-muted">{types.get(b.type)?.label ?? b.type} · {b.field}</span>
            </li>
          {/each}
          {#if hidden > 0}<li class="h-muted">{t('collections.hiddenRefs', { n: hidden })}</li>{/if}
        </ul>
      </div>
    </div>
  {/if}
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (delOpen = false)}>{t('common.cancel')}</Button>
    <Button variant="danger" loading={delBusy} disabled={!!blockers} onclick={() => void doDelete()}>{t('trash.move')}</Button>
  {/snippet}
</Dialog>

<Dialog bind:open={unpOpen} title={t('release.unpublishTitle')} size="sm" dismissible={!unpBusy}>
  {#if data}<p>{t('release.unpublishText', { name: data.name || data.id })}</p>{/if}
  {#if unpError}<p class="h-alert" role="alert">{unpError}</p>{/if}
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (unpOpen = false)}>{t('common.cancel')}</Button>
    <Button variant="danger" loading={unpBusy} onclick={() => void doUnpublish()}>{t('release.unpublish')}</Button>
  {/snippet}
</Dialog>

<ScheduleDialog
  bind:open={schedOpen}
  title={t(schedAction === 'publish' ? 'schedule.titlePublish' : 'schedule.titleUnpublish')}
  text={data ? t(schedAction === 'publish' ? 'schedule.textPublish' : 'schedule.textUnpublish', { name: data.name || data.id }) : undefined}
  onconfirm={doSchedule} />

{#if data}
  <AddToReleaseDialog bind:open={addOpen} item={data.id} itemName={data.name || data.id} locales={localeParts} onadded={onscheduled} />
{/if}

<style>
  .h-adlg__parts {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    margin: var(--h-space-3) 0;
    padding: var(--h-space-3);
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius);
  }
  .h-adlg__blockers ul {
    margin: 0;
    padding-left: var(--h-space-4);
  }
</style>
