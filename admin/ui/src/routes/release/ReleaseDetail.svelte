<script lang="ts">
  import { tick as settle } from 'svelte';
  import '../../lib/messages/releases';
  import { ApiError } from '../../lib/api/client';
  import { api } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { errorMessage, reportError } from '../../lib/errors';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import { types } from '../../lib/types.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Card from '../../lib/ui/Card.svelte';
  import Combobox, { type ComboOption } from '../../lib/ui/Combobox.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import Textarea from '../../lib/ui/Textarea.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Confirm from '../settings/Confirm.svelte';
  import { releaseApi } from './api';
  import ScheduleDialog from './ScheduleDialog.svelte';
  import {
    actionCounts,
    canCancel,
    canDeleteRelease,
    canPublish,
    canSchedule,
    editable,
    formatInZone,
    itemStatus,
    localZone,
    statusTone,
    type Release,
    type ReleaseItem,
  } from './logic';

  let { params }: { params?: Record<string, string> } = $props();
  const id = $derived(params?.id ?? router.params.id ?? '');

  let rel = $state.raw<Release | null>(null);
  let loadError = $state<Error | null>(null);
  let tick = $state(0);
  let busy = $state(false);

  $effect(() => {
    void tick;
    const rid = id;
    const ctl = new AbortController();
    loadError = null;
    releaseApi
      .get(rid, ctl.signal)
      .then(async (r) => {
        rel = r;
        if (focusNext) {
          const sel = focusNext;
          focusNext = '';
          await settle();
          document.querySelector<HTMLElement>(sel)?.focus();
        }
        if (!dirty) {
          name = r.name;
          description = r.description;
        }
      })
      .catch((e) => {
        if (!ctl.signal.aborted) loadError = e;
      });
    return () => ctl.abort();
  });
  // Where focus goes once the reload lands, after an action unmounted the control that had it.
  let focusNext = '';
  const reload = () => tick++;

  const canManage = $derived(session.can('release.manage'));
  const canPub = $derived(canManage && session.can('content.publish'));
  const isEditable = $derived(!!rel && canManage && editable(rel.status));

  // ---- name and description
  let name = $state('');
  let description = $state('');
  let saving = $state(false);
  let saveError = $state('');
  const dirty = $derived(!!rel && (name !== rel.name || description !== rel.description));

  async function saveDetails() {
    if (!rel) return;
    if (!name.trim()) {
      saveError = t('releases.nameRequired');
      return;
    }
    saving = true;
    saveError = '';
    try {
      rel = await releaseApi.update(rel.id, { name: name.trim(), description: description.trim() });
      name = rel.name;
      description = rel.description;
      toast.success(t('releases.saved'));
      reload();
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500 || e.status === 0) throw e;
      saveError = errorMessage(e);
    } finally {
      saving = false;
    }
  }

  // ---- run an action, show the server's refusal, reload
  let stateErr = false;
  async function act(fn: () => Promise<unknown>, done?: string): Promise<boolean> {
    busy = true;
    stateErr = false;
    try {
      await fn();
      if (done) toast.success(done);
      return true;
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500 || e.status === 0) throw e;
      stateErr = e.code === 'release_state';
      if (stateErr) toast.error(t('releases.stateChanged'));
      else reportError(e);
      return false;
    } finally {
      busy = false;
      reload();
    }
  }

  // ---- items
  let pick = $state<ComboOption | null>(null);
  const readable = $derived(types.listable.filter((x) => session.can('content.read', { type: x.name })));
  async function search(q: string, signal: AbortSignal): Promise<ComboOption[]> {
    const per = await Promise.all(
      readable.map((ty) =>
        api
          .get('/collections/{type}/items', { params: { type: ty.name }, query: { q, limit: 6 } as never, signal })
          .then((r) => r.items.map((i) => ({ value: i.id, label: i.name || i.id, hint: ty.label })))
          .catch((e) => {
            if (signal.aborted) throw e;
            return [] as ComboOption[];
          }),
      ),
    );
    return per.flat();
  }
  async function addItem(o: ComboOption | null) {
    if (!o || !rel) return;
    await act(() => releaseApi.putItem(rel!.id, o.value, { action: 'publish' }), t('releases.itemAdded', { name: o.label }));
    pick = null;
  }
  async function setAction(it: ReleaseItem, action: string) {
    if (!rel || (action !== 'publish' && action !== 'unpublish')) return;
    await act(() => releaseApi.putItem(rel!.id, it.item, { action, ...(it.locales.length ? { locales: it.locales } : {}) }));
  }
  async function removeItem(it: ReleaseItem) {
    if (!rel) return;
    focusNext = '#rel-items-h';
    if (!(await act(() => releaseApi.removeItem(rel!.id, it.item), t('releases.itemRemoved', { name: it.name || it.item })))) focusNext = '';
  }

  async function unschedule() {
    if (!rel) return;
    focusNext = '[data-focus=schedule]';
    if (!(await act(() => releaseApi.unschedule(rel!.id), t('releases.unscheduled')))) focusNext = '';
  }

  // ---- publish, schedule, unschedule, cancel, delete
  let pubOpen = $state(false);
  let schedOpen = $state(false);
  let cancelOpen = $state(false);
  let deleteOpen = $state(false);
  const counts = $derived(actionCounts(rel?.items ?? []));

  async function publishNow(force = false) {
    if (!rel) return;
    busy = true;
    try {
      await releaseApi.publish(rel.id, force);
      toast.success(t('releases.publishedNow'));
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500 || e.status === 0) throw e;
      // A failed publish is recorded on the release: the failure panel below lists why.
      if (e.code === 'release_state') toast.error(t('releases.stateChanged'));
      else if (e.code !== 'invalid' && e.code !== 'conflict' && e.code !== 'release_failed') reportError(e);
      else toast.error(t('releases.publishFailed'));
    } finally {
      busy = false;
      pubOpen = false;
      reload();
    }
  }

  const itemName = (item?: string) => rel?.items.find((i) => i.item === item)?.name || item || '';
  const hasConflict = $derived(!!rel?.errors.some((e) => e.code === 'conflict'));
  const zone = localZone();
  </script>

{#if loadError}
  <div class="h-page">
    {#if (loadError as ApiError).status === 404}
      <EmptyState icon="send" title={t('releases.notFound')}>
        {#snippet actions()}<a class="h-button h-button--secondary h-button--md" use:link href={href('/releases')}>{t('nav.releases')}</a>{/snippet}
      </EmptyState>
    {:else}
      <div class="h-alert" role="alert">
        <span>{errorMessage(loadError)}</span>
        <Button size="sm" onclick={reload}>{t('app.retry')}</Button>
      </div>
    {/if}
  </div>
{:else if !rel}
  <div class="h-page" aria-busy="true"><Skeleton height="2rem" width="40%" /><Skeleton shape="block" height="14rem" /></div>
{:else}
  <div class="h-page">
    <PageHeader
      title={rel.name}
      breadcrumbs={[{ label: t('nav.publishing') }, { label: t('nav.releases'), href: '/releases' }, { label: rel.name }]}
      {badge}
      meta={meta}
      {actions}
      overflow={canManage && canDeleteRelease(rel.status) ? [{ label: t('releases.delete'), icon: 'trash', danger: true, onselect: () => (deleteOpen = true) }] : []}
      {primary} />

    {#if rel.status === 'failed' && rel.errors.length}
      <div class="h-alert h-rel__fail" role="alert" aria-label={t('releases.failureTitle')}>
        <div>
          <p><strong>{t('releases.failureTitle')}</strong></p>
          <p>{t('releases.failureText')}</p>
          <ul>
            {#each rel.errors as e, i (i)}
              <li>
                {#if e.item}<a use:link href={href(`/items/${e.item}`)}>{itemName(e.item)}</a>{:else}<span>{t('releases.wholeRelease')}</span>{/if}
                {#if e.code}<code class="h-mono">{e.code}</code>{/if}
                {#if e.message}<span>{e.message}</span>{/if}
                {#if e.path}<span class="h-muted">{e.path}</span>{/if}
              </li>
            {/each}
          </ul>
          {#if canPub && hasConflict}
            <Button size="sm" variant="danger" loading={busy} onclick={() => void publishNow(true)}>{t('releases.publishAnyway')}</Button>
          {/if}
        </div>
      </div>
    {/if}

    {#if rel.status === 'scheduled'}<p class="h-muted" role="status">{t('releases.lockedScheduled')}</p>{/if}

    <Card title={t('releases.details')}>
      <form
        class="h-stack h-rel__form"
        onsubmit={(e) => {
          e.preventDefault();
          void saveDetails();
        }}>
        <Field label={t('releases.name')} required>
          {#snippet children(c)}<Input {...c} bind:value={name} disabled={!isEditable} />{/snippet}
        </Field>
        <Field label={t('releases.description')}>
          {#snippet children(c)}<Textarea {...c} rows={2} bind:value={description} disabled={!isEditable} />{/snippet}
        </Field>
        {#if saveError}<p class="h-alert" role="alert">{saveError}</p>{/if}
        {#if isEditable}
          <div><Button type="submit" variant="secondary" loading={saving} disabled={!dirty}>{t('common.save')}</Button></div>
        {/if}
      </form>
    </Card>

    <section class="h-rel__items" aria-labelledby="rel-items-h">
      <h2 id="rel-items-h" class="h-rel__h" tabindex="-1">{t('releases.itemsTitle', { n: rel.items.length })}</h2>
      {#if isEditable}
        <div class="h-rel__add">
          <Field label={t('releases.addItems')}>
            {#snippet children(c)}
              <Combobox {...c} value={pick} {search} clearable={false} placeholder={t('releases.addPlaceholder')} onselect={(o) => void addItem(o)} />
            {/snippet}
          </Field>
        </div>
      {/if}
      <Table
        caption={t('releases.itemsCaption')}
        rows={rel.items}
        rowKey={(r) => r.item}
        columns={[
          { key: 'name', label: t('releases.itemName'), cell: itemCell },
          { key: 'type', label: t('releases.itemType'), cell: typeCell, mobile: 'hide' },
          { key: 'action', label: t('releases.action'), cell: actionCell },
          { key: 'locales', label: t('releases.locales'), cell: localesCell, mobile: 'hide' },
          { key: 'state', label: t('releases.itemStatus'), cell: stateCell },
          ...(isEditable ? [{ key: 'remove', label: t('common.actions'), align: 'end' as const, cell: removeCell }] : []),
        ]}>
        {#snippet empty()}<EmptyState icon="file" title={t('releases.noItems')} description={isEditable ? t('releases.noItemsHint') : undefined} />{/snippet}
      </Table>
    </section>
  </div>

  <ScheduleDialog
    bind:open={schedOpen}
    title={t('schedule.titleRelease')}
    text={t('schedule.textRelease', { name: rel.name })}
    initial={rel.scheduledAt}
    onconfirm={async (at) => {
      await releaseApi.schedule(rel!.id, at);
      toast.success(t('releases.scheduledDone'));
      reload();
    }} />

  <Dialog bind:open={pubOpen} title={t('releases.publishNow')} size="sm" dismissible={!busy}>
    <p>{t('releases.publishConfirm', { publish: counts.publish, unpublish: counts.unpublish })}</p>
    <p class="h-muted">{t('releases.publishAtomic')}</p>
    {#snippet footer()}
      <Button variant="ghost" disabled={busy} onclick={() => (pubOpen = false)}>{t('common.cancel')}</Button>
      <Button variant="primary" loading={busy} onclick={() => void publishNow()}>{t('releases.publishNowConfirm')}</Button>
    {/snippet}
  </Dialog>

  <Confirm
    bind:open={cancelOpen}
    title={t('releases.cancel')}
    message={t('releases.cancelConfirm', { name: rel.name })}
    confirm={t('releases.cancel')}
    onconfirm={async () => {
      focusNext = '#rel-items-h';
      // A release_state refusal means it changed under us: close, the reload shows the new state.
      if (!(await act(() => releaseApi.cancel(rel!.id), t('releases.cancelled'))) && !stateErr) throw new Error('refused');
    }} />

  <Confirm
    bind:open={deleteOpen}
    title={t('releases.delete')}
    message={t('releases.deleteConfirm', { name: rel.name })}
    confirm={t('releases.delete')}
    onconfirm={async () => {
      try {
        await releaseApi.remove(rel!.id);
      } catch (e) {
        reportError(e);
        throw e;
      }
      toast.success(t('releases.deleted'));
      void router.navigate('/releases', { force: true });
    }} />
{/if}

{#snippet badge()}
  {#if rel}<Badge tone={statusTone(rel.status)} dot>{t(`releases.status.${rel.status}`)}</Badge>{/if}
{/snippet}

{#snippet meta()}
  {#if rel}
    {rel.kind === 'item' ? t('releases.kindItem') : t('releases.kindRelease')}
    {#if rel.status === 'scheduled' && rel.scheduledAt}{' · '}{t('releases.scheduledFor', { date: formatInZone(rel.scheduledAt), zone })}{/if}
    {#if rel.status === 'published' && rel.publishedAt}{' · '}{t('releases.publishedAt', { date: formatInZone(rel.publishedAt) })}{/if}
    {#if rel.createdByName}{' · '}{t('releases.createdBy', { name: rel.createdByName })}{/if}
  {/if}
{/snippet}

{#snippet actions()}
  {#if rel && canManage}
    {#if canSchedule(rel.status)}<Button icon="clock" data-focus="schedule" disabled={busy || rel.items.length === 0} onclick={() => (schedOpen = true)}>{t('schedule.menu')}</Button>{/if}
    {#if rel.status === 'scheduled'}<Button loading={busy} onclick={() => void unschedule()}>{t('releases.unschedule')}</Button>{/if}
    {#if canCancel(rel.status)}<Button variant="ghost" disabled={busy} onclick={() => (cancelOpen = true)}>{t('releases.cancel')}</Button>{/if}
  {/if}
{/snippet}

{#snippet primary()}
  {#if rel && canPub && canPublish(rel.status)}
    <Button variant="primary" icon="send" disabled={busy || rel.items.length === 0} onclick={() => (pubOpen = true)}>{t('releases.publishNow')}</Button>
  {/if}
{/snippet}

{#snippet itemCell(r: ReleaseItem)}
  {#if r.state === 'trashed'}
    <a use:link href={href(`/trash/${r.item}`)}>{r.name || r.item}</a>
  {:else if r.state === 'missing' || r.state === 'hidden'}
    <span class="h-muted">{r.name || r.item}</span>
  {:else}
    <a use:link href={href(`/items/${r.item}`)}>{r.name || r.item}</a>
  {/if}
{/snippet}
{#snippet typeCell(r: ReleaseItem)}{types.get(r.type)?.label ?? r.type}{/snippet}
{#snippet actionCell(r: ReleaseItem)}
  {#if isEditable}
    <Select
      compact
      aria-label={t('releases.actionOf', { name: r.name || r.item })}
      value={r.action}
      options={[
        { value: 'publish', label: t('releases.action.publish') },
        { value: 'unpublish', label: t('releases.action.unpublish') },
      ]}
      onchange={(v) => void setAction(r, v)} />
  {:else}
    {t(`releases.action.${r.action}`)}
  {/if}
{/snippet}
{#snippet localesCell(r: ReleaseItem)}{r.locales.length ? r.locales.join(', ') : '—'}{/snippet}
{#snippet stateCell(r: ReleaseItem)}{@const st = itemStatus(r)}<Badge tone={st.tone} dot>{t(st.key)}</Badge>{/snippet}
{#snippet removeCell(r: ReleaseItem)}
  <IconButton icon="x" size="sm" label={t('releases.removeItem', { name: r.name || r.item })} onclick={() => void removeItem(r)} />
{/snippet}

<style>
  .h-rel__form {
    max-width: 40rem;
  }
  .h-rel__fail ul {
    margin: var(--h-space-2) 0;
    padding-left: var(--h-space-4);
  }
  .h-rel__items {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-3);
  }
  .h-rel__h {
    margin: 0;
    font-size: var(--h-text-lg);
  }
  .h-rel__add {
    max-width: 28rem;
  }
</style>
