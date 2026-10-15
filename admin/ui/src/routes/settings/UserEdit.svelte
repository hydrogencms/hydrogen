<script lang="ts">
  import '../../lib/messages/settings';
  import { untrack } from 'svelte';
  import { api, ApiError, type Schemas } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { formatDateTime } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Breadcrumbs from '../../lib/ui/Breadcrumbs.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Tabs from '../../lib/ui/Tabs.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Assignments from './Assignments.svelte';
  import Confirm from './Confirm.svelte';
  import SecretBox from './SecretBox.svelte';
  import { generatePassword, reportError } from '../../lib/errors';
  import { submitForm } from './util';

  let { params }: { params: Record<string, string> } = $props();
  const id = $derived(params.id);

  let user = $state<Schemas['User'] | null>(null);
  let failed = $state(false);
  let tab = $state(router.query.get('tab') ?? 'details');
  const form = new Form({ name: '', email: '' });
  form.guard();

  let groups = $state<Schemas['Group'][] | null>(null);
  let allGroups = $state<Schemas['Group'][]>([]);
  let addGroup = $state('');

  let confirmDisable = $state(false);
  let confirmDelete = $state(false);
  let resetOpen = $state(false);
  let newPassword = $state('');
  let shownPassword = $state('');
  let resetError = $state('');
  let resetting = $state(false);

  async function load() {
    failed = false;
    try {
      user = await api.get('/users/{id}', { params: { id } });
      form.reset({ name: user.name, email: user.email });
    } catch (e) {
      failed = true;
      reportError(e);
    }
  }
  // params are cleared while the router leaves this route: only load for a real id.
  $effect(() => {
    if (id) untrack(() => void load());
  });

  async function save() {
    if (await submitForm(form, async (v) => (user = await api.patch('/users/{id}', { params: { id }, body: v })))) {
      toast.success(t('settings.saved'));
    }
  }

  async function setDisabled(disabled: boolean) {
    try {
      user = await api.patch('/users/{id}', { params: { id }, body: { disabled } });
      toast.success(t(disabled ? 'settings.users.wasDisabled' : 'settings.users.wasEnabled'));
    } catch (e) {
      reportError(e);
      throw e;
    }
  }

  async function remove() {
    try {
      await api.del('/users/{id}', { params: { id } });
    } catch (e) {
      reportError(e);
      throw e;
    }
    toast.success(t('settings.users.deleted'));
    await router.navigate('/settings/users', { replace: true });
  }

  function openReset() {
    newPassword = '';
    shownPassword = '';
    resetError = '';
    resetOpen = true;
  }
  async function reset() {
    const pw = newPassword || generatePassword();
    resetting = true;
    resetError = '';
    try {
      await api.patch('/users/{id}', { params: { id }, body: { password: pw } });
      shownPassword = pw;
      toast.success(t('settings.users.passwordReset'));
    } catch (e) {
      if (e instanceof ApiError && e.status < 500 && e.status !== 403) resetError = e.message;
      else reportError(e);
    } finally {
      resetting = false;
    }
  }

  async function loadGroups() {
    try {
      const [all, mine] = await Promise.all([
        api.get('/groups', { query: { limit: 500 } }),
        api.get('/users/{id}/groups', { params: { id } }),
      ]);
      allGroups = all.items;
      groups = mine.items;
    } catch (e) {
      reportError(e);
      groups = [];
    }
  }
  $effect(() => {
    if (tab === 'groups' && groups === null) void loadGroups();
  });
  const joinable = $derived(allGroups.filter((g) => !groups?.some((x) => x.id === g.id)));

  async function join() {
    if (!addGroup) return;
    try {
      await api.put('/groups/{id}/members/{user}', { params: { id: addGroup, user: id } });
      addGroup = '';
      groups = null;
      await loadGroups();
    } catch (e) {
      reportError(e);
    }
  }
  async function leave(g: Schemas['Group']) {
    try {
      await api.del('/groups/{id}/members/{user}', { params: { id: g.id, user: id } });
      groups = groups?.filter((x) => x.id !== g.id) ?? null;
    } catch (e) {
      reportError(e);
    }
  }
</script>

<div class="h-page">
  <Breadcrumbs items={[{ label: t('nav.users'), href: '/settings/users' }, { label: user?.login ?? '…' }]} />
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else if !user}
    <Skeleton lines={5} height="1.4rem" />
  {:else}
    <div class="h-page__head">
      <h1>{user.login}</h1>
      {#if user.disabled}<Badge tone="warning">{t('settings.users.disabled')}</Badge>{/if}
    </div>
    <Tabs
      label={user.login}
      bind:value={tab}
      tabs={[
        { id: 'details', label: t('settings.tab.details') },
        { id: 'groups', label: t('nav.groups') },
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
              <Field label={t('settings.users.login')}>
                {#snippet children(c)}<Input {...c} value={user!.login} readonly />{/snippet}
              </Field>
              <Field label={t('settings.users.name')} error={form.error('name')}>
                {#snippet children(c)}<Input {...c} bind:value={form.values.name} oninput={() => form.clearError('name')} />{/snippet}
              </Field>
              <Field label={t('settings.users.email')} error={form.error('email')}>
                {#snippet children(c)}<Input {...c} type="email" bind:value={form.values.email} oninput={() => form.clearError('email')} />{/snippet}
              </Field>
              {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
              <div class="h-row">
                <Button type="submit" variant="primary" loading={form.pending} disabled={!form.dirty}>{t('common.save')}</Button>
              </div>
            </form>
            <p class="h-muted">
              {t('settings.users.lastLogin')}: {user!.lastLoginAt ? formatDateTime(user!.lastLoginAt) : t('settings.users.never')} ·
              {t('settings.created')}: {formatDateTime(user!.createdAt)}
              {#if !user!.hasPassword} · {t('settings.users.noPassword')}{/if}
            </p>
            <section class="h-card h-stack" aria-labelledby="u-acc">
              <h2 class="h-card__title" id="u-acc">{t('settings.users.access')}</h2>
              <div class="h-row">
                <Button onclick={openReset}>{t('settings.users.resetPassword')}</Button>
                {#if user!.disabled}
                  <Button onclick={() => setDisabled(false)}>{t('settings.users.enable')}</Button>
                {:else}
                  <Button onclick={() => (confirmDisable = true)}>{t('settings.users.disable')}</Button>
                {/if}
                <span class="h-spacer"></span>
                <Button variant="danger" icon="trash" onclick={() => (confirmDelete = true)}>{t('common.delete')}</Button>
              </div>
            </section>
          </div>
        {:else if active === 'groups'}
          <div class="h-stack h-narrow">
            {#if groups === null}
              <Skeleton lines={3} height="1.4rem" />
            {:else}
              {#if groups.length === 0}<p class="h-muted">{t('settings.users.noGroups')}</p>{/if}
              <ul class="h-list h-plain">
                {#each groups as g (g.id)}
                  <li class="h-list__item">
                    <span>{g.name}</span>
                    <Button size="sm" onclick={() => leave(g)}>{t('settings.groups.remove')}</Button>
                  </li>
                {/each}
              </ul>
              <div class="h-row">
                <Field label={t('settings.users.addToGroup')}>
                  {#snippet children(c)}
                    <Select {...c} bind:value={addGroup} placeholder={t('settings.users.pickGroup')} options={joinable.map((g) => ({ value: g.id, label: g.name }))} />
                  {/snippet}
                </Field>
                <Button variant="primary" disabled={!addGroup} onclick={join}>{t('settings.groups.add')}</Button>
              </div>
            {/if}
          </div>
        {:else}
          <Assignments kind="user" {id} />
        {/if}
      {/snippet}
    </Tabs>
  {/if}
</div>

<Confirm
  bind:open={confirmDisable}
  title={t('settings.users.disable')}
  message={t('settings.users.disableConfirm')}
  confirm={t('settings.users.disable')}
  onconfirm={() => setDisabled(true)} />
<Confirm
  bind:open={confirmDelete}
  title={t('common.delete')}
  message={t('settings.users.deleteConfirm', { login: user?.login ?? '' })}
  confirm={t('common.delete')}
  onconfirm={remove} />

<Dialog bind:open={resetOpen} title={t('settings.users.resetPassword')} size="sm">
  {#if shownPassword}
    <SecretBox value={shownPassword} warning={t('settings.users.shownOnce')} />
  {:else}
    <form
      id="reset-form"
      class="h-stack"
      onsubmit={(e) => {
        e.preventDefault();
        void reset();
      }}>
      <Field label={t('settings.users.newPassword')} description={t('settings.users.generateHint')} error={resetError}>
        {#snippet children(c)}<Input {...c} type="text" autocomplete="off" bind:value={newPassword} />{/snippet}
      </Field>
      <Button onclick={() => (newPassword = generatePassword())}>{t('settings.users.generate')}</Button>
    </form>
  {/if}
  {#snippet footer()}
    {#if shownPassword}
      <Button variant="primary" onclick={() => (resetOpen = false)}>{t('common.close')}</Button>
    {:else}
      <Button onclick={() => (resetOpen = false)}>{t('common.cancel')}</Button>
      <Button type="submit" form="reset-form" variant="primary" loading={resetting}>{t('settings.users.resetPassword')}</Button>
    {/if}
  {/snippet}
</Dialog>

<style>
  .h-narrow {
    max-width: 40rem;
  }
</style>
