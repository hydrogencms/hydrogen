<script lang="ts">
  import '../../lib/messages/settings';
  import { untrack } from 'svelte';
  import { api, ApiError, type Schemas } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import Badge from '../../lib/ui/Badge.svelte';
  import Card from '../../lib/ui/Card.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Time from '../../lib/ui/Time.svelte';
  import DetailLayout from './DetailLayout.svelte';
  import { href, link, router } from '../../lib/router.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
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
    if (id) untrack(() => void loadGroups());
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

  const overflow = $derived(
    user
      ? [
          { label: t('settings.users.resetPassword'), icon: 'key', onselect: openReset },
          user.disabled
            ? { label: t('settings.users.enable'), icon: 'check-circle', onselect: () => void setDisabled(false).catch(() => {}) }
            : { label: t('settings.users.disable'), icon: 'lock', onselect: () => (confirmDisable = true) },
          { label: t('common.delete'), icon: 'trash', danger: true, separator: true, onselect: () => (confirmDelete = true) },
        ]
      : [],
  );
</script>

<div class="h-page">
  <PageHeader
    title={user ? user.name || user.login : t('nav.users')}
    breadcrumbs={[{ label: t('nav.settings') }, { label: t('nav.users'), href: '/settings/users' }, { label: user?.login ?? '…' }]}
    {overflow}>
    {#snippet badge()}{#if user?.disabled}<Badge tone="warning" dot>{t('settings.users.disabled')}</Badge>{/if}{/snippet}
    {#snippet meta()}
      {#if user}
        {user.login} · {t('settings.users.lastLogin')}:
        {#if user.lastLoginAt}<Time at={user.lastLoginAt} />{:else}{t('settings.users.never')}{/if}
      {/if}
    {/snippet}
    {#snippet primary()}
      {#if user}<Button type="submit" form="user-form" variant="primary" loading={form.pending} disabled={!form.dirty}>{t('common.save')}</Button>{/if}
    {/snippet}
  </PageHeader>
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else if !user}
    <DetailLayout>
      {#snippet main()}<Card><Skeleton lines={5} height="1.4rem" /></Card>{/snippet}
      {#snippet side()}<Card><Skeleton lines={3} height="1.4rem" /></Card><Card><Skeleton lines={3} height="1.4rem" /></Card>{/snippet}
    </DetailLayout>
  {:else}
    {@const u = user}
    <DetailLayout>
      {#snippet main()}
        <Card title={t('settings.users.profile')}>
          <form
            id="user-form"
            class="h-stack h-form-narrow"
            onsubmit={(e) => {
              e.preventDefault();
              void save();
            }}>
            <Field label={t('settings.users.login')}>
              {#snippet children(c)}<Input {...c} value={u.login} readonly />{/snippet}
            </Field>
            <Field label={t('settings.users.name')} error={form.error('name')}>
              {#snippet children(c)}<Input {...c} bind:value={form.values.name} oninput={() => form.clearError('name')} />{/snippet}
            </Field>
            <Field label={t('settings.users.email')} error={form.error('email')}>
              {#snippet children(c)}<Input {...c} type="email" bind:value={form.values.email} oninput={() => form.clearError('email')} />{/snippet}
            </Field>
            {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
          </form>
        </Card>
        <Card title={t('settings.users.access')}>
          <dl class="h-facts">
            <dt>{t('settings.users.status')}</dt>
            <dd>{#if u.disabled}<Badge tone="warning" dot>{t('settings.users.disabled')}</Badge>{:else}<Badge tone="success" dot>{t('settings.users.active')}</Badge>{/if}</dd>
            <dt>{t('settings.users.lastLogin')}</dt>
            <dd>{#if u.lastLoginAt}<Time at={u.lastLoginAt} />{:else}{t('settings.users.never')}{/if}</dd>
            <dt>{t('settings.created')}</dt>
            <dd><Time at={u.createdAt} /></dd>
            <dt>{t('settings.users.password')}</dt>
            <dd>{u.hasPassword ? t('settings.users.passwordSet') : t('settings.users.noPassword')}</dd>
          </dl>
        </Card>
      {/snippet}
      {#snippet side()}
        <Card title={t('nav.groups')} padding="sm">
          {#if groups === null}
            <Skeleton lines={3} height="1.4rem" />
          {:else}
            {#if groups.length === 0}<p class="h-muted">{t('settings.users.noGroups')}</p>{/if}
            <ul class="h-list h-plain">
              {#each groups as g (g.id)}
                <li class="h-list__item">
                  <a use:link href={href(`/settings/groups/${g.id}`)}>{g.name}</a>
                  <IconButton icon="x" size="sm" label={t('settings.users.leaveGroup', { name: g.name })} onclick={() => leave(g)} />
                </li>
              {/each}
            </ul>
            <div class="h-row h-addrow">
              <Field label={t('settings.users.addToGroup')}>
                {#snippet children(c)}
                  <Select {...c} bind:value={addGroup} placeholder={t('settings.users.pickGroup')} options={joinable.map((g) => ({ value: g.id, label: g.name }))} />
                {/snippet}
              </Field>
              <Button disabled={!addGroup} onclick={join}>{t('settings.groups.add')}</Button>
            </div>
          {/if}
        </Card>
        <Assignments kind="user" {id} />
      {/snippet}
    </DetailLayout>
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
  .h-form-narrow {
    max-width: 40rem;
  }
  .h-facts {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--h-space-3) var(--h-space-5);
    margin: 0;
    font-size: var(--h-text-md);
  }
  .h-facts dt {
    color: var(--h-text-muted);
  }
  .h-facts dd {
    margin: 0;
  }
  .h-addrow {
    align-items: end;
    margin-top: var(--h-space-3);
  }
  .h-addrow :global(.h-field) {
    flex: 1;
  }
</style>
