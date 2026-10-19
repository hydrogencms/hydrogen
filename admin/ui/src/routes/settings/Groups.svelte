<script lang="ts">
  import Slot from '../../lib/ext/Slot.svelte';
  import '../../lib/messages/settings';
  import { api, type Schemas } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import { session } from '../../lib/auth/session.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import NameCell from '../../lib/ui/NameCell.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import ListBar from './ListBar.svelte';
  import SettingsHead from './SettingsHead.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { matchesText, PAGE, pageOf, submitForm } from './util';

  type Group = Schemas['Group'];
  const page = $derived(Math.max(1, Number(router.query.get('page')) || 1));
  let rows = $state<Group[] | null>(null);
  let q = $state(router.query.get('q') ?? '');
  let failed = $state(false);
  let creating = $state(false);
  const form = new Form({ name: '', description: '' });

  // Groups are few: load them all, filter and page here.
  async function load() {
    failed = false;
    try {
      rows = (await api.get('/groups', { query: { limit: 500 } })).items;
    } catch {
      failed = true;
    }
  }
  $effect(() => {
    void load();
  });
  const filtered = $derived((rows ?? []).filter((r) => matchesText(q, r.name, r.description)));
  function typing() {
    router.setQuery({ q: q.trim() || undefined, page: undefined });
  }

  async function create() {
    if (!(await submitForm(form, (v) => api.post('/groups', { body: v })))) return;
    creating = false;
    toast.success(t('settings.groups.created'));
    await load();
  }
</script>

<div class="h-page">
  <SettingsHead section="groups">
    {#snippet primary()}
      {#if session.can('user.manage')}
        <Button
          variant="primary"
          icon="plus"
          onclick={() => {
            form.reset({ name: '', description: '' });
            creating = true;
          }}>{t('settings.groups.new')}</Button>
      {/if}
    {/snippet}
  </SettingsHead>
  <ListBar bind:value={q} label={t('settings.groups.search')} placeholder={t('settings.groups.search')} oninput={typing} />
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t('nav.groups')}
      loading={!rows}
      rows={pageOf(filtered, page)}
      rowKey={(r) => r.id}
      columns={[
        { key: 'name', label: t('settings.groups.name'), cell: nameCell },
        { key: 'description', label: t('settings.description'), mobile: 'hide' },
        { key: 'createdAt', label: t('settings.created'), cell: createdCell },
      ]}>
      {#snippet empty()}
        {#if q.trim()}
          <EmptyState icon="search" title={t('settings.groups.noMatch')} />
        {:else}
          <EmptyState icon="layers" title={t('settings.groups.empty')} description={t('settings.groups.emptyHint')} />
        {/if}
      {/snippet}
      {#snippet footer()}
        <Pagination {page} pageSize={PAGE} total={filtered.length} onpage={(p) => router.setQuery({ page: p === 1 ? undefined : p }, { push: true })} />
      {/snippet}
    </Table>
  {/if}
  <Slot at="settings.after" props={{section: 'groups'}} />
</div>

{#snippet nameCell(r: Group)}<NameCell name={r.name} icon="layers" href={`/settings/groups/${r.id}`} />{/snippet}
{#snippet createdCell(r: Group)}<Time at={r.createdAt} />{/snippet}

<Dialog bind:open={creating} title={t('settings.groups.new')}>
  <form
    id="group-new"
    class="h-stack"
    onsubmit={(e) => {
      e.preventDefault();
      void create();
    }}>
    <Field label={t('settings.groups.name')} error={form.error('name')} required>
      {#snippet children(c)}<Input {...c} bind:value={form.values.name} oninput={() => form.clearError('name')} />{/snippet}
    </Field>
    <Field label={t('settings.description')} error={form.error('description')}>
      {#snippet children(c)}<Input {...c} bind:value={form.values.description} />{/snippet}
    </Field>
    {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={() => (creating = false)}>{t('common.cancel')}</Button>
    <Button type="submit" form="group-new" variant="primary" loading={form.pending}>{t('common.create')}</Button>
  {/snippet}
</Dialog>
