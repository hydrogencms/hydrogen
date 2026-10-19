<script lang="ts">
  import Slot from '../../lib/ext/Slot.svelte';
  import '../../lib/messages/settings';
  import { api, type Schemas } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import { session } from '../../lib/auth/session.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
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
  import { PAGE, submitForm } from './util';

  type User = Schemas['User'];
  const page = $derived(Math.max(1, Number(router.query.get('page')) || 1));
  let rows = $state<User[] | null>(null);
  let total = $state(0);
  let failed = $state(false);
  let creating = $state(false);
  let q = $state(router.query.get('q') ?? '');
  const applied = $derived(router.query.get('q') ?? '');
  let seq = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const form = new Form({ login: '', name: '', email: '', password: '' });

  async function load(p: number, search: string) {
    const mine = ++seq;
    failed = false;
    try {
      const r = await api.get('/users', { query: { q: search.trim() || undefined, limit: PAGE, offset: (p - 1) * PAGE } });
      if (mine !== seq) return;
      rows = r.items;
      total = r.total;
    } catch {
      if (mine === seq) failed = true;
    }
  }
  $effect(() => {
    void load(page, applied);
  });
  function typing() {
    clearTimeout(timer);
    timer = setTimeout(() => router.setQuery({ q: q.trim() || undefined, page: undefined }), 250);
  }

  function openCreate() {
    form.reset({ login: '', name: '', email: '', password: '' });
    creating = true;
  }
  async function create() {
    const made = await submitForm(form, (v) =>
      api.post('/users', { body: { login: v.login, name: v.name, email: v.email, ...(v.password ? { password: v.password } : {}) } }),
    );
    if (!made) return;
    creating = false;
    toast.success(t('settings.users.created'));
    await load(page, applied);
  }
</script>

<div class="h-page">
  <SettingsHead section="users">
    {#snippet primary()}
      {#if session.can('user.manage')}<Button variant="primary" icon="plus" onclick={openCreate}>{t('settings.users.new')}</Button>{/if}
    {/snippet}
  </SettingsHead>
  <ListBar bind:value={q} label={t('settings.users.search')} placeholder={t('settings.users.search')} oninput={typing} />
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={() => load(page, applied)}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t('nav.users')}
      loading={!rows}
      rows={rows ?? []}
      rowKey={(r) => r.id}
      columns={[
        { key: 'name', label: t('settings.users.name'), cell: nameCell },
        { key: 'email', label: t('settings.users.email'), mobile: 'hide' },
        { key: 'disabled', label: t('settings.users.status'), cell: statusCell },
        { key: 'lastLoginAt', label: t('settings.users.lastLogin'), cell: lastCell },
      ]}>
      {#snippet empty()}
        {#if applied}
          <EmptyState icon="search" title={t('settings.users.noMatch')} description={t('settings.users.noMatchHint')} />
        {:else}
          <EmptyState icon="users" title={t('settings.users.empty')} description={t('settings.users.emptyHint')} />
        {/if}
      {/snippet}
      {#snippet footer()}
        <Pagination {page} pageSize={PAGE} {total} onpage={(p) => router.setQuery({ page: p === 1 ? undefined : p }, { push: true })} />
      {/snippet}
    </Table>
  {/if}
  <Slot at="settings.after" props={{section: 'users'}} />
</div>

{#snippet nameCell(r: User)}<NameCell name={r.name || r.login} sub={r.name && r.name !== r.login ? r.login : undefined} avatar={r.name || r.login} href={`/settings/users/${r.id}`} />{/snippet}
{#snippet statusCell(r: User)}
  {#if r.disabled}<Badge tone="warning" dot>{t('settings.users.disabled')}</Badge>{:else}<Badge tone="success" dot>{t('settings.users.active')}</Badge>{/if}
{/snippet}
{#snippet lastCell(r: User)}{#if r.lastLoginAt}<Time at={r.lastLoginAt} />{:else}<span class="h-muted">{t('settings.users.never')}</span>{/if}{/snippet}

<Dialog bind:open={creating} title={t('settings.users.new')}>
  <form
    id="user-new"
    class="h-stack"
    onsubmit={(e) => {
      e.preventDefault();
      void create();
    }}>
    <Field label={t('settings.users.login')} error={form.error('login')} required>
      {#snippet children(c)}<Input {...c} bind:value={form.values.login} autocomplete="off" oninput={() => form.clearError('login')} />{/snippet}
    </Field>
    <Field label={t('settings.users.name')} error={form.error('name')}>
      {#snippet children(c)}<Input {...c} bind:value={form.values.name} oninput={() => form.clearError('name')} />{/snippet}
    </Field>
    <Field label={t('settings.users.email')} error={form.error('email')}>
      {#snippet children(c)}<Input {...c} type="email" bind:value={form.values.email} oninput={() => form.clearError('email')} />{/snippet}
    </Field>
    <Field label={t('settings.users.password')} description={t('settings.users.passwordOptional')} error={form.error('password')}>
      {#snippet children(c)}<Input {...c} type="password" autocomplete="new-password" bind:value={form.values.password} oninput={() => form.clearError('password')} />{/snippet}
    </Field>
    {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={() => (creating = false)}>{t('common.cancel')}</Button>
    <Button type="submit" form="user-new" variant="primary" loading={form.pending}>{t('common.create')}</Button>
  {/snippet}
</Dialog>
