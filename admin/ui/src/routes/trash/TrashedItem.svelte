<script lang="ts">
  import '../../lib/messages/releases';
  import '../../lib/messages/collections';
  import '../../lib/messages/editor';
  import { ApiError } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { provideFields } from '../../lib/fields/context';
  import Fields from '../../lib/fields/Fields.svelte';
  import { hydrate, type Doc } from '../../lib/fields/logic';
  import { errorMessage, reportError } from '../../lib/errors';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import { types, type CompiledField } from '../../lib/types.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Card from '../../lib/ui/Card.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Confirm from '../settings/Confirm.svelte';
  import { trashApi } from '../release/api';

  let { params }: { params?: Record<string, string> } = $props();
  const id = $derived(params?.id ?? router.params.id ?? '');

  interface Trashed {
    id: string;
    type: string;
    name: string;
    locale: string;
    fields: Record<string, unknown>;
    deletedAt?: string;
    deletedBy?: string;
    deletedByName?: string;
    actions?: { delete?: boolean };
  }

  let data = $state.raw<Trashed | null>(null);
  let doc = $state<Doc>({});
  let loadError = $state<Error | null>(null);
  let busy = $state(false);
  let purgeOpen = $state(false);

  $effect(() => {
    const rid = id;
    const ctl = new AbortController();
    loadError = null;
    void types.load();
    trashApi
      .read(rid, router.query.get('locale') ?? undefined, ctl.signal)
      .then((d: Trashed) => {
        data = d;
        doc = hydrate(types.get(d.type)?.fields ?? [], d.fields);
      })
      .catch((e) => {
        if (!ctl.signal.aborted) loadError = e;
      });
    return () => ctl.abort();
  });

  const type = $derived(types.get(data?.type ?? ''));
  const canDelete = $derived(session.can('content.delete') && (data?.actions?.delete ?? true));

  provideFields({
    error: () => undefined,
    clearError: () => {},
    readonly: true,
    get locale() {
      return data?.locale ?? '';
    },
    isNew: false,
  });

  const groups = $derived.by(() => {
    const m = new Map<string, CompiledField[]>();
    for (const f of type?.fields ?? []) {
      if (f.admin?.hidden) continue;
      const g = f.group ?? '';
      m.set(g, [...(m.get(g) ?? []), f]);
    }
    return [...m].map(([g, fields]) => ({ title: g || t('editor.content'), fields }));
  });

  async function restore() {
    if (!data) return;
    busy = true;
    try {
      await trashApi.restore(data.id);
      toast.success(t('trash.restored', { name: data.name || data.id }));
      void router.navigate(`/items/${data.id}`, { force: true });
    } catch (e) {
      if (e instanceof ApiError) reportError(e);
      else throw e;
    } finally {
      busy = false;
    }
  }
  async function purge() {
    if (!data) return;
    try {
      await trashApi.purge(data.id);
    } catch (e) {
      reportError(e);
      throw e;
    }
    toast.success(t('trash.purged'));
    void router.navigate('/trash', { force: true });
  }
</script>

{#if loadError}
  <div class="h-page">
    {#if (loadError as ApiError).status === 404}
      <EmptyState icon="trash" title={t('trash.notFound')} description={t('trash.notFoundText')}>
        {#snippet actions()}<a class="h-button h-button--secondary h-button--md" use:link href={href('/trash')}>{t('nav.trash')}</a>{/snippet}
      </EmptyState>
    {:else}
      <div class="h-alert" role="alert">{errorMessage(loadError)}</div>
    {/if}
  </div>
{:else if !data || !type}
  <div class="h-page" aria-busy="true"><Skeleton height="2rem" width="40%" /><Skeleton shape="block" height="14rem" /></div>
{:else}
  <div class="h-page">
    <PageHeader
      title={data.name || t('collections.untitled')}
      breadcrumbs={[{ label: t('nav.publishing') }, { label: t('nav.trash'), href: '/trash' }, { label: data.name || data.id }]}
      meta={type.label}>
      {#snippet badge()}<Badge tone="danger" dot>{t('trash.badge')}</Badge>{/snippet}
    </PageHeader>

    <div class="h-alert h-trashed__banner" role="status">
      <div>
        <p>
          <strong>{t('trash.bannerTitle')}</strong>
          {#if data.deletedAt}<span>{' '}{t('trash.bannerWhen')} <Time at={data.deletedAt} /></span>{/if}
          {#if data.deletedByName || data.deletedBy}<span>{' '}{t('trash.bannerBy', { name: data.deletedByName || data.deletedBy || '' })}</span>{/if}
        </p>
        <p>{t('trash.bannerText')}</p>
      </div>
      {#if canDelete}
        <div class="h-trashed__acts">
          <Button icon="refresh" loading={busy} onclick={() => void restore()}>{t('trash.restore')}</Button>
          <Button variant="danger" disabled={busy} onclick={() => (purgeOpen = true)}>{t('trash.purge')}</Button>
        </div>
      {/if}
    </div>

    <form class="h-item__form" aria-label={t('collections.formLabel', { type: type.label })} onsubmit={(e) => e.preventDefault()}>
      {#each groups as g (g.title)}
        <Card title={g.title} class="h-item__card">
          <div class="h-fieldcard">
            <div class="h-fieldgrid">
              <Fields fields={g.fields} bind:doc />
            </div>
          </div>
        </Card>
      {/each}
    </form>
  </div>

  <Confirm
    bind:open={purgeOpen}
    title={t('trash.purge')}
    message={t('trash.purgeConfirm', { name: data.name || data.id })}
    confirm={t('trash.purge')}
    onconfirm={purge} />
{/if}

<style>
  .h-trashed__banner {
    align-items: center;
    justify-content: space-between;
    gap: var(--h-space-4);
    flex-wrap: wrap;
  }
  .h-trashed__banner p {
    margin: 0;
  }
  .h-trashed__acts {
    display: flex;
    gap: var(--h-space-2);
  }
  .h-item__form {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-4);
  }
</style>
