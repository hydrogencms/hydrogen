<script lang="ts">
  import '../../lib/messages/settings';
  import { api, type Schemas } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import { session } from '../../lib/auth/session.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { PAGE, submitForm } from './util';

  type Group = Schemas['Group'];
  const page = $derived(Math.max(1, Number(router.query.get('page')) || 1));
  let rows = $state<Group[] | null>(null);
  let total = $state(0);
  let failed = $state(false);
  let creating = $state(false);
  const form = new Form({ name: '', description: '' });

  async function load(p: number) {
    failed = false;
    try {
      const r = await api.get('/groups', { query: { limit: PAGE, offset: (p - 1) * PAGE } });
      rows = r.items;
      total = r.total;
    } catch {
      failed = true;
    }
  }
  $effect(() => {
    void load(page);
  });

  async function create() {
    if (!(await submitForm(form, (v) => api.post('/groups', { body: v })))) return;
    creating = false;
    toast.success(t('settings.groups.created'));
    await load(page);
  }
</script>

<div class="h-page">
  <div class="h-page__head">
    <h1>{t('nav.groups')}</h1>
    {#if session.can('user.manage')}
      <Button
        variant="primary"
        icon="plus"
        onclick={() => {
          form.reset({ name: '', description: '' });
          creating = true;
        }}>{t('settings.groups.new')}</Button>
    {/if}
  </div>
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={() => load(page)}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t('nav.groups')}
      loading={!rows}
      rows={rows ?? []}
      rowKey={(r) => r.id}
      columns={[
        { key: 'name', label: t('settings.groups.name'), cell: nameCell },
        { key: 'description', label: t('settings.description') },
      ]}>
      {#snippet empty()}<p class="h-muted">{t('settings.groups.empty')}</p>{/snippet}
    </Table>
    <Pagination {page} pageSize={PAGE} {total} onpage={(p) => router.setQuery({ page: p === 1 ? undefined : p }, { push: true })} />
  {/if}
</div>

{#snippet nameCell(r: Group)}<a use:link href={href(`/settings/groups/${r.id}`)}>{r.name}</a>{/snippet}

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
