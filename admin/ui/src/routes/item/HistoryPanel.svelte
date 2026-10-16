<script lang="ts">
  import '../../lib/messages/history';
  import { api } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { errorMessage } from '../../lib/errors';
  import type { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import type { CompiledType } from '../../lib/types.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Checkbox from '../../lib/ui/Checkbox.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { untrack } from 'svelte';
  import { reviewApi } from '../review/api';
  import { reviewRev } from '../review/revision.svelte';
  import type { ReviewEvent } from '../review/logic';
  import { actorName } from './actors';
  import { collectAudit, mergeActivity, type Activity, type AuditPage, type AuditRow, type VersionMeta } from './activity';
  import type { ItemData } from './model';

  let {
    data,
    type,
    form,
    editLocked,
    onrefresh,
  }: {
    data: ItemData;
    type: CompiledType;
    form: Form<{ fields: Record<string, unknown>; key: string; alwaysAvailable: boolean }>;
    editLocked: boolean;
    onrefresh?: () => void;
  } = $props();

  // Props are getters on the parent's object: key the loads on derived scalars.
  const id = $derived(data.id);
  const stamp = $derived(`${data.version}|${data.updatedAt}|${data.status}`);
  const current = $derived(data.version);
  let tick = $state(0);

  let versions = $state.raw<VersionMeta[]>([]);
  let audit = $state.raw<AuditRow[]>([]);
  let review = $state.raw<ReviewEvent[] | null>(null);
  let loading = $state(true);
  let error = $state('');
  let partial = $state(false);
  // Older audit entries exist than the ones read so far.
  let auditMore = $state(false);
  let auditNext: number | undefined;
  let loadingOlder = $state(false);
  let shownFor = '';

  const auditPage = (itemId: string, signal?: AbortSignal) => (before?: number) =>
    api.get('/audit', { query: { subject: itemId, limit: 100, before } as never, signal }) as Promise<AuditPage>;

  $effect(() => {
    const itemId = id;
    void stamp;
    void tick;
    void reviewRev.n;
    const wantsReview = !!type.review;
    const ctl = new AbortController();
    // A reload of the same item keeps the timeline on screen.
    if (shownFor !== itemId) loading = true;
    error = '';
    partial = false;
    (async () => {
      try {
        const v = await api.get('/items/{id}/versions', { params: { id: itemId }, signal: ctl.signal });
        if (ctl.signal.aborted) return;
        versions = v.items as VersionMeta[];
      } catch (e) {
        if (!ctl.signal.aborted) error = errorMessage(e);
      }
      // The audit log and the review thread need their own rights: missing ones just leave those rows out.
      const [a, r] = await Promise.all([
        collectAudit(auditPage(itemId, ctl.signal)).catch((e) => (e?.status !== 403 && (partial = true), { rows: [] as AuditRow[], hasMore: false, next: undefined })),
        wantsReview ? reviewApi.get(itemId, ctl.signal).then((x) => x.events).catch((e) => (e?.status !== 403 && (partial = true), null)) : Promise.resolve(null),
      ]);
      if (ctl.signal.aborted) return;
      audit = a.rows;
      auditMore = a.hasMore;
      auditNext = a.next;
      review = r;
      shownFor = itemId;
      loading = false;
    })();
    return () => ctl.abort();
  });

  async function older() {
    if (loadingOlder || auditNext === undefined) return;
    loadingOlder = true;
    const itemId = id;
    try {
      const a = await collectAudit(auditPage(itemId), { before: auditNext });
      if (itemId !== id) return;
      audit = [...audit, ...a.rows];
      auditMore = a.hasMore;
      auditNext = a.next;
    } catch (e) {
      toast.error(errorMessage(e));
    } finally {
      loadingOlder = false;
    }
  }

  const SHOWN = 15;
  let all = $state(false);
  const activity = $derived(mergeActivity({ versions, audit, review }));
  const shown = $derived(all ? activity : activity.slice(0, SHOWN));

  // Names of whoever acted, resolved once each.
  let who = $state.raw<Record<string, string>>({});
  const asked = new Set<string>();
  $effect(() => {
    const refs = [...new Set([...activity.map((a) => a.actor), ...versions.map((v) => v.createdBy)].filter(Boolean))];
    for (const ref of refs) {
      if (asked.has(ref)) continue;
      asked.add(ref);
      void nameOf(ref).then((n) => (who = { ...untrack(() => who), [ref]: n }));
    }
  });
  async function nameOf(ref: string): Promise<string> {
    const [kind, uid = ''] = ref.split(':');
    if (kind === 'user' && uid && uid === session.principal?.id) return t('history.who.you');
    if (kind === 'token') return t('history.who.token');
    if (kind === 'agent') return t('history.who.agent');
    if (kind === 'system') return t('history.who.system');
    return actorName(ref);
  }
  const whoOf = (a: Activity) => a.actorName || who[a.actor] || (a.actor ? '' : t('history.who.someone'));
  const verb = (a: Activity) => {
    const key = a.kind === 'review' ? `history.ev.review.${a.what}` : `history.act.${a.what}`;
    return t(key, a.vars) !== key ? t(key, a.vars) : a.what.replace(/[._]+/g, ' ');
  };

  // ---- compare
  function compare(n: number) {
    void router.navigate(`/items/${id}/compare?a=${n}&b=draft`);
  }

  // ---- restore
  let target = $state(0);
  let open = $state(false);
  let chosen = $state<string[]>([]);
  let busy = $state(false);
  let failure = $state('');
  const targetMeta = $derived(versions.find((v) => v.version === target));
  const offered = $derived(targetMeta?.locales ?? []);
  const hasLocales = $derived(type.fields.some((f) => f.translatable) && offered.length > 0);

  function ask(n: number) {
    target = n;
    chosen = versions.find((v) => v.version === n)?.locales.slice() ?? [];
    failure = '';
    open = true;
  }
  const toggle = (l: string, on: boolean) => (chosen = on ? [...new Set([...chosen, l])] : chosen.filter((x) => x !== l));

  async function restore() {
    if (busy) return;
    busy = true;
    failure = '';
    try {
      await api.post('/items/{id}/versions/{n}/restore' as never, { params: { id, n: target }, body: hasLocales ? { locales: chosen } : {} } as never);
      open = false;
      toast.success(t('history.restore.done', { n: target }));
      // The dialog warned about unsaved edits: they are replaced, so reloading needs no second question.
      if (form.dirty) form.markSaved();
      onrefresh?.();
      tick++;
    } catch (e) {
      failure = errorMessage(e);
    } finally {
      busy = false;
    }
  }
</script>

{#if loading}
  <Skeleton lines={4} />
{:else}
  {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
  {#if partial}<p class="h-muted h-hist__note">{t('history.activity.failed')}</p>{/if}

  <h3 class="h-hist__h" id="h-hist-activity">{t('history.activity')}</h3>
  {#if activity.length === 0}
    <p class="h-muted">{t('history.activity.empty')}</p>
  {:else}
    <ol class="h-hist__timeline" aria-labelledby="h-hist-activity">
      {#each shown as a (a.key)}
        <li class="h-hist__ev">
          <span class="h-hist__node h-hist__node--{a.kind}" aria-hidden="true"></span>
          <span><strong>{whoOf(a)}</strong> {verb(a)}</span>
          <span class="h-muted"><Time at={a.at} /></span>
          {#if a.comment}<blockquote class="h-hist__quote">{a.comment}</blockquote>{/if}
        </li>
      {/each}
    </ol>
    {#if !all && activity.length > SHOWN}
      <Button variant="ghost" size="sm" onclick={() => (all = true)}>{t('history.activity.more')}</Button>
    {:else if auditMore}
      <p class="h-muted h-hist__note">{t('history.activity.older')}</p>
      <Button variant="ghost" size="sm" loading={loadingOlder} onclick={() => void older()}>{t('history.activity.loadOlder')}</Button>
    {/if}
  {/if}

  <h3 class="h-hist__h" id="h-hist-versions">{t('history.versions')}</h3>
  {#if versions.length === 0}
    <p class="h-muted">{t('collections.versions.none')}</p>
  {:else}
    <ol class="h-hist__versions" aria-labelledby="h-hist-versions">
      {#each versions as v (v.version)}
        <li class="h-hist__ver">
          <div class="h-hist__line">
            <strong id="h-hist-v{v.version}">{t('editor.meta.version', { n: v.version })}</strong>
            {#if v.version === current}<Badge tone="success">{t('collections.versions.current')}</Badge>{/if}
            {#if v.locales.length}<span class="h-muted">{v.locales.join(', ')}</span>{/if}
          </div>
          <span class="h-muted"><Time at={v.createdAt} />{#if who[v.createdBy]}{' '}{t('editor.history.by', { who: who[v.createdBy]! })}{/if}</span>
          <div class="h-hist__acts">
            <Button variant="ghost" size="sm" aria-describedby="h-hist-v{v.version}" onclick={() => compare(v.version)}>{t('history.compare')}</Button>
            {#if !editLocked}
              <Button variant="ghost" size="sm" aria-describedby="h-hist-v{v.version}" onclick={() => ask(v.version)}>{t('history.restore')}</Button>
            {/if}
          </div>
        </li>
      {/each}
    </ol>
  {/if}
{/if}

<Dialog bind:open title={t('history.restore.title')} dismissible={!busy}>
  <p>{t('history.restore.text', { n: target })}</p>
  {#if hasLocales}
    <fieldset class="h-hist__locales">
      <legend>{t('history.restore.locales')}</legend>
      {#each offered as l (l)}
        <Checkbox checked={chosen.includes(l)} onchange={(e) => toggle(l, (e.currentTarget as HTMLInputElement).checked)}>
          {l}{#if l === data.mainLocale}{' '}<span class="h-muted">({t('collections.mainLocale')})</span>{/if}
        </Checkbox>
      {/each}
    </fieldset>
  {/if}
  {#if form.dirty}<p class="h-alert" role="status">{t('history.restore.dirty')}</p>{/if}
  {#if type.review}<p class="h-muted">{t('history.restore.review')}</p>{/if}
  {#if failure}<p class="h-alert" role="alert">{failure}</p>{/if}
  {#snippet footer()}
    <Button variant="ghost" onclick={() => (open = false)}>{t('common.cancel')}</Button>
    <Button variant="primary" loading={busy} disabled={hasLocales && chosen.length === 0} onclick={() => void restore()}>{t('history.restore.go')}</Button>
  {/snippet}
</Dialog>

<style>
  .h-hist__h {
    margin: var(--h-space-4) 0 var(--h-space-2);
    font-size: var(--h-text-sm);
    font-weight: 600;
  }
  .h-hist__h:first-child {
    margin-top: 0;
  }
  .h-hist__note {
    font-size: var(--h-text-xs);
  }
  .h-hist__timeline,
  .h-hist__versions {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: var(--h-space-3);
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: var(--h-text-sm);
  }
  .h-hist__timeline::before {
    content: '';
    position: absolute;
    top: 8px;
    bottom: 8px;
    left: 4px;
    width: 1px;
    background: var(--h-border-strong);
  }
  .h-hist__ev {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding-left: var(--h-space-5);
  }
  .h-hist__node {
    position: absolute;
    top: 5px;
    left: 0;
    width: 9px;
    height: 9px;
    border: 2px solid var(--h-border-strong);
    border-radius: 50%;
    background: var(--h-surface);
  }
  .h-hist__node--version {
    border-color: var(--h-success);
    background: var(--h-success);
  }
  .h-hist__node--review {
    border-color: var(--h-info);
  }
  .h-hist__quote {
    margin: 2px 0 0;
    padding-left: var(--h-space-3);
    border-left: 2px solid var(--h-border-strong);
    color: var(--h-text-muted);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .h-hist__ver {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding-bottom: var(--h-space-3);
    border-bottom: 1px solid var(--h-border);
  }
  .h-hist__ver:last-child {
    border-bottom: 0;
  }
  .h-hist__line {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--h-space-2);
  }
  .h-hist__acts {
    display: flex;
    gap: var(--h-space-1);
    margin-left: calc(var(--h-space-3) * -1);
  }
  .h-hist__locales {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    margin: var(--h-space-3) 0;
    padding: 0;
    border: 0;
  }
  .h-hist__locales legend {
    padding: 0;
    margin-bottom: var(--h-space-1);
    font-weight: 600;
  }
</style>
