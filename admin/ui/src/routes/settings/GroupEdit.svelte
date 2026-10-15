<script lang="ts">
  import '../../lib/messages/settings';
  import { untrack } from 'svelte';
  import { api, type Schemas } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import Breadcrumbs from '../../lib/ui/Breadcrumbs.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Combobox, { type ComboOption } from '../../lib/ui/Combobox.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import Tabs from '../../lib/ui/Tabs.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Assignments from './Assignments.svelte';
  import Confirm from './Confirm.svelte';
  import { reportError } from '../../lib/errors';
  import { submitForm } from './util';

  let { params }: { params: Record<string, string> } = $props();
  const id = $derived(params.id);

  let group = $state<Schemas['Group'] | null>(null);
  let failed = $state(false);
  let tab = $state(router.query.get('tab') ?? 'details');
  const form = new Form({ name: '', description: '' });
  form.guard();
  let members = $state<Schemas['User'][] | null>(null);
  let picked = $state<ComboOption | null>(null);
  let confirmDelete = $state(false);

  async function load() {
    failed = false;
    try {
      group = await api.get('/groups/{id}', { params: { id } });
      form.reset({ name: group.name, description: group.description });
    } catch (e) {
      failed = true;
      reportError(e);
    }
  }
  $effect(() => {
    if (id) untrack(() => void load());
  });

  async function save() {
    if (await submitForm(form, async (v) => (group = await api.patch('/groups/{id}', { params: { id }, body: v })))) {
      toast.success(t('settings.saved'));
    }
  }
  async function remove() {
    try {
      await api.del('/groups/{id}', { params: { id } });
    } catch (e) {
      reportError(e);
      throw e;
    }
    toast.success(t('settings.groups.deleted'));
    await router.navigate('/settings/groups', { replace: true });
  }

  async function loadMembers() {
    try {
      members = (await api.get('/groups/{id}/members', { params: { id }, query: { limit: 500 } })).items;
    } catch (e) {
      reportError(e);
      members = [];
    }
  }
  $effect(() => {
    if (tab === 'members' && members === null) void loadMembers();
  });

  async function searchUsers(q: string, signal: AbortSignal): Promise<ComboOption[]> {
    const r = await api.get('/users', { query: { q: q.trim() || undefined, limit: 20 }, signal });
    return r.items
      .filter((u) => !members?.some((m) => m.id === u.id))
      .map((u) => ({ value: u.id, label: u.login, hint: u.name || u.email }));
  }
  async function addMember() {
    if (!picked) return;
    try {
      await api.put('/groups/{id}/members/{user}', { params: { id, user: picked.value } });
      picked = null;
      await loadMembers();
    } catch (e) {
      reportError(e);
    }
  }
  async function removeMember(u: Schemas['User']) {
    try {
      await api.del('/groups/{id}/members/{user}', { params: { id, user: u.id } });
      members = members?.filter((m) => m.id !== u.id) ?? null;
    } catch (e) {
      reportError(e);
    }
  }
</script>

<div class="h-page">
  <Breadcrumbs items={[{ label: t('nav.groups'), href: '/settings/groups' }, { label: group?.name ?? '…' }]} />
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else if !group}
    <Skeleton lines={4} height="1.4rem" />
  {:else}
    <div class="h-page__head"><h1>{group.name}</h1></div>
    <Tabs
      label={group.name}
      bind:value={tab}
      tabs={[
        { id: 'details', label: t('settings.tab.details') },
        { id: 'members', label: t('settings.groups.members'), count: members?.length },
        { id: 'roles', label: t('nav.roles') },
      ]}>
      {#snippet children(active)}
        {#if active === 'details'}
          <div class="h-stack h-narrow">
            <form
              class="h-stack"
              onsubmit={(e) => {
                e.preventDefault();
                void save();
              }}>
              <Field label={t('settings.groups.name')} error={form.error('name')} required>
                {#snippet children(c)}<Input {...c} bind:value={form.values.name} oninput={() => form.clearError('name')} />{/snippet}
              </Field>
              <Field label={t('settings.description')} error={form.error('description')}>
                {#snippet children(c)}<Input {...c} bind:value={form.values.description} />{/snippet}
              </Field>
              {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
              <div class="h-row">
                <Button type="submit" variant="primary" loading={form.pending} disabled={!form.dirty}>{t('common.save')}</Button>
                <span class="h-spacer"></span>
                <Button variant="danger" icon="trash" onclick={() => (confirmDelete = true)}>{t('common.delete')}</Button>
              </div>
            </form>
          </div>
        {:else if active === 'members'}
          <div class="h-stack h-narrow">
            <div class="h-row">
              <div class="h-grow">
                <Field label={t('settings.groups.addMember')}>
                  {#snippet children(c)}
                    <Combobox {...c} bind:value={picked} search={searchUsers} placeholder={t('settings.groups.searchUsers')} />
                  {/snippet}
                </Field>
              </div>
              <Button variant="primary" disabled={!picked} onclick={addMember}>{t('settings.groups.add')}</Button>
            </div>
            <Table
              caption={t('settings.groups.members')}
              loading={members === null}
              rows={members ?? []}
              rowKey={(r) => r.id}
              columns={[
                { key: 'login', label: t('settings.users.login'), cell: memberCell },
                { key: 'name', label: t('settings.users.name') },
                { key: 'actions', label: t('common.actions'), align: 'end', cell: removeCell },
              ]}>
              {#snippet empty()}<p class="h-muted">{t('settings.groups.noMembers')}</p>{/snippet}
            </Table>
          </div>
        {:else}
          <Assignments kind="group" {id} />
        {/if}
      {/snippet}
    </Tabs>
  {/if}
</div>

{#snippet memberCell(u: Schemas['User'])}<a use:link href={href(`/settings/users/${u.id}`)}>{u.login}</a>{/snippet}
{#snippet removeCell(u: Schemas['User'])}<Button size="sm" onclick={() => removeMember(u)}>{t('settings.groups.remove')}</Button>{/snippet}

<Confirm
  bind:open={confirmDelete}
  title={t('common.delete')}
  message={t('settings.groups.deleteConfirm', { name: group?.name ?? '' })}
  confirm={t('common.delete')}
  onconfirm={remove} />

<style>
  .h-narrow {
    max-width: 40rem;
  }
  .h-grow {
    flex: 1;
  }
</style>
