<script lang="ts">
  import '../../lib/messages/settings';
  import { untrack } from 'svelte';
  import { api, type Schemas } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import Avatar from '../../lib/ui/Avatar.svelte';
  import Card from '../../lib/ui/Card.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import DetailLayout from './DetailLayout.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Combobox, { type ComboOption } from '../../lib/ui/Combobox.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Assignments from './Assignments.svelte';
  import Confirm from './Confirm.svelte';
  import { reportError } from '../../lib/errors';
  import { submitForm } from './util';

  let { params }: { params: Record<string, string> } = $props();
  const id = $derived(params.id);

  let group = $state<Schemas['Group'] | null>(null);
  let failed = $state(false);
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
    if (id) untrack(() => void loadMembers());
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
  const overflow = $derived(group ? [{ label: t('common.delete'), icon: 'trash', danger: true, onselect: () => (confirmDelete = true) }] : []);
</script>

<div class="h-page">
  <PageHeader
    title={group?.name ?? t('nav.groups')}
    breadcrumbs={[{ label: t('nav.settings') }, { label: t('nav.groups'), href: '/settings/groups' }, { label: group?.name ?? '…' }]}
    meta={group?.description || undefined}
    {overflow}>
    {#snippet primary()}
      {#if group}<Button type="submit" form="group-form" variant="primary" loading={form.pending} disabled={!form.dirty}>{t('common.save')}</Button>{/if}
    {/snippet}
  </PageHeader>
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else if !group}
    <DetailLayout>
      {#snippet main()}<Card><Skeleton lines={4} height="1.4rem" /></Card>{/snippet}
      {#snippet side()}<Card><Skeleton lines={3} height="1.4rem" /></Card><Card><Skeleton lines={3} height="1.4rem" /></Card>{/snippet}
    </DetailLayout>
  {:else}
    <DetailLayout>
      {#snippet main()}
        <Card title={t('settings.tab.details')}>
          <form
            id="group-form"
            class="h-stack h-form-narrow"
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
          </form>
        </Card>
      {/snippet}
      {#snippet side()}
        <Card title={t('settings.groups.members')} padding="sm">
          <div class="h-row h-addrow">
            <div class="h-grow">
              <Field label={t('settings.groups.addMember')}>
                {#snippet children(c)}
                  <Combobox {...c} bind:value={picked} search={searchUsers} placeholder={t('settings.groups.searchUsers')} />
                {/snippet}
              </Field>
            </div>
            <Button disabled={!picked} onclick={addMember}>{t('settings.groups.add')}</Button>
          </div>
          {#if members === null}
            <Skeleton lines={3} height="1.4rem" />
          {:else if members.length === 0}
            <p class="h-muted">{t('settings.groups.noMembers')}</p>
          {:else}
            <ul class="h-list h-plain">
              {#each members as u (u.id)}
                <li class="h-list__item">
                  <span class="h-row">
                    <Avatar name={u.name || u.login} size={28} />
                    <a use:link href={href(`/settings/users/${u.id}`)}>{u.name || u.login}</a>
                    {#if u.name}<span class="h-muted">{u.login}</span>{/if}
                  </span>
                  <IconButton icon="x" size="sm" label={t('settings.groups.removeMember', { name: u.name || u.login })} onclick={() => removeMember(u)} />
                </li>
              {/each}
            </ul>
          {/if}
        </Card>
        <Assignments kind="group" {id} />
      {/snippet}
    </DetailLayout>
  {/if}
</div>

<Confirm
  bind:open={confirmDelete}
  title={t('common.delete')}
  message={t('settings.groups.deleteConfirm', { name: group?.name ?? '' })}
  confirm={t('common.delete')}
  onconfirm={remove} />

<style>
  .h-form-narrow {
    max-width: 40rem;
  }
  .h-grow {
    flex: 1;
    min-width: 0;
  }
  .h-addrow {
    align-items: end;
    margin-bottom: var(--h-space-3);
  }
</style>
