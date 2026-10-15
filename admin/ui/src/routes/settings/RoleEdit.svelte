<script lang="ts">
  import '../../lib/messages/settings';
  import { untrack } from 'svelte';
  import { api, type Schemas } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Breadcrumbs from '../../lib/ui/Breadcrumbs.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import Tabs from '../../lib/ui/Tabs.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Confirm from './Confirm.svelte';
  import LimitationEditor from './LimitationEditor.svelte';
  import {
    ALL, addPolicy, addable, applicableTo, cleanPolicies, groupPolicies, removePolicy,
    type LimitDef, type PolicyDef, type RolePolicy,
  } from './limits';
  import { reportError } from '../../lib/errors';
  import { loadCatalog, submitForm } from './util';

  let { params }: { params: Record<string, string> } = $props();
  const id = $derived(params.id);
  const isNew = $derived(id === 'new');

  let role = $state<Schemas['Role'] | null>(null);
  let failed = $state(false);
  let catalog = $state<{ policies: PolicyDef[]; limitations: LimitDef[] } | null>(null);
  let tab = $state(router.query.get('tab') ?? 'policies');
  const form = new Form<{ name: string; description: string; policies: RolePolicy[] }>({ name: '', description: '', policies: [] });
  form.guard();
  let toAdd = $state('');
  let confirmDelete = $state(false);
  let holders = $state<{ a: Schemas['Assignment']; who: string; to: string }[] | null>(null);

  const normalize = (ps: Schemas['RolePolicy'][]): RolePolicy[] => ps.map((p) => ({ policy: p.policy, limitations: p.limitations ?? {} }));

  async function load() {
    failed = false;
    try {
      catalog = await loadCatalog();
      if (isNew) return;
      role = await api.get('/roles/{id}', { params: { id } });
      form.reset({ name: role.name, description: role.description, policies: normalize(role.policies) });
    } catch (e) {
      failed = true;
      reportError(e);
    }
  }
  $effect(() => {
    if (id) untrack(() => void load());
  });

  const groups = $derived(groupPolicies(addable(catalog?.policies ?? [], form.values.policies)));
  const everything = $derived(form.values.policies.some((p) => p.policy === ALL));
  const describe = (name: string) => (name === ALL ? t('settings.roles.everything') : catalog?.policies.find((p) => p.name === name)?.description ?? '');

  function add() {
    if (!toAdd) return;
    form.values.policies = addPolicy(form.values.policies, toAdd);
    toAdd = '';
  }
  function limitErrors(i: number): Record<string, string | undefined> {
    const out: Record<string, string | undefined> = {};
    const prefix = `policies[${i}].limitations.`;
    for (const [k, v] of Object.entries(form.errors)) if (k.startsWith(prefix)) out[k.slice(prefix.length)] = v;
    return out;
  }

  async function save() {
    let made: Schemas['Role'] | undefined;
    const ok = await submitForm(form, async (v) => {
      const body = { name: v.name, description: v.description, policies: cleanPolicies(v.policies) };
      made = isNew ? await api.post('/roles', { body }) : await api.patch('/roles/{id}', { params: { id }, body });
    });
    if (!ok || !made) return;
    toast.success(t('settings.saved'));
    if (isNew) await router.navigate(`/settings/roles/${made.id}`, { replace: true });
    else {
      role = made;
      form.reset({ name: made.name, description: made.description, policies: normalize(made.policies) });
    }
  }
  async function remove() {
    try {
      await api.del('/roles/{id}', { params: { id } });
    } catch (e) {
      reportError(e);
      throw e;
    }
    toast.success(t('settings.roles.deleted'));
    await router.navigate('/settings/roles', { replace: true });
  }

  async function loadHolders() {
    try {
      const [as, users, grps, toks] = await Promise.all([
        api.get('/roles/{id}/assignments', { params: { id } }),
        api.get('/users', { query: { limit: 500 } }).catch(() => null),
        api.get('/groups', { query: { limit: 500 } }).catch(() => null),
        api.get('/tokens', { query: { limit: 500 } }).catch(() => null),
      ]);
      const name = new Map<string, string>();
      for (const u of users?.items ?? []) name.set(u.id, u.login);
      for (const g of grps?.items ?? []) name.set(g.id, g.name);
      for (const k of toks?.items ?? []) name.set(k.id, k.name);
      holders = as.items.map((a) => ({
        a,
        who: name.get(a.principalId) ?? a.principalId,
        to: a.principalKind === 'user' ? `/settings/users/${a.principalId}` : a.principalKind === 'group' ? `/settings/groups/${a.principalId}` : '/settings/tokens',
      }));
    } catch (e) {
      reportError(e);
      holders = [];
    }
  }
  $effect(() => {
    if (!isNew && tab === 'assignments' && holders === null) void loadHolders();
  });
  const limitText = (l: Record<string, string[]>) =>
    Object.keys(l).sort().map((k) => `${k}: ${l[k].join(', ')}`).join('; ');
</script>

<div class="h-page">
  <Breadcrumbs items={[{ label: t('nav.roles'), href: '/settings/roles' }, { label: isNew ? t('settings.roles.new') : (role?.name ?? '…') }]} />
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else if !catalog || (!isNew && !role)}
    <Skeleton lines={6} height="1.4rem" />
  {:else}
    <div class="h-page__head">
      <h1>{isNew ? t('settings.roles.new') : role!.name}</h1>
      {#if role?.template}<Badge tone="info">{t('settings.roles.fromTemplate', { name: role.template })}</Badge>{/if}
    </div>
    <Tabs
      label={t('nav.roles')}
      bind:value={tab}
      tabs={[
        { id: 'policies', label: t('settings.roles.policies') },
        { id: 'assignments', label: t('settings.roles.holders'), disabled: isNew },
      ]}>
      {#snippet children(active)}
        {#if active === 'policies'}
          <form
            class="h-stack h-wide"
            onsubmit={(e) => {
              e.preventDefault();
              void save();
            }}>
            <Field label={t('settings.roles.name')} error={form.error('name')} required>
              {#snippet children(c)}<Input {...c} bind:value={form.values.name} oninput={() => form.clearError('name')} />{/snippet}
            </Field>
            <Field label={t('settings.description')} error={form.error('description')}>
              {#snippet children(c)}<Input {...c} bind:value={form.values.description} />{/snippet}
            </Field>

            <h2>{t('settings.roles.policies')}</h2>
            {#if everything}<p class="h-muted">{t('settings.roles.everythingHint')}</p>{/if}
            {#if form.values.policies.length === 0}<p class="h-muted">{t('settings.roles.noPolicies')}</p>{/if}
            <ul class="h-stack h-roles__list">
              {#each form.values.policies as rp, i (i)}
                <li class="h-card h-stack" data-policy={rp.policy}>
                  <div class="h-row">
                    <strong>{rp.policy === ALL ? t('settings.roles.everythingName') : rp.policy}</strong>
                    <span class="h-muted">{describe(rp.policy)}</span>
                    <span class="h-spacer"></span>
                    <IconButton
                      icon="trash"
                      label={t('settings.roles.removePolicy', { policy: rp.policy })}
                      variant="danger"
                      onclick={() => (form.values.policies = removePolicy(form.values.policies, i))} />
                  </div>
                  {#if form.error(`policies[${i}].policy`)}<p class="h-field__error" role="alert">{form.error(`policies[${i}].policy`)}</p>{/if}
                  <LimitationEditor
                    names={applicableTo(rp.policy, catalog!.policies, catalog!.limitations)}
                    defs={catalog!.limitations}
                    bind:value={form.values.policies[i].limitations}
                    errors={limitErrors(i)}
                    prefix={`p${i}`} />
                </li>
              {/each}
            </ul>

            <div class="h-row">
              <label class="h-sr" for="role-add-policy">{t('settings.roles.addPolicy')}</label>
              <select id="role-add-policy" class="h-input h-select h-roles__pick" bind:value={toAdd}>
                <option value="">{t('settings.roles.addPolicy')}</option>
                {#if !everything}<option value={ALL}>{t('settings.roles.everythingName')}</option>{/if}
                {#each groups as g (g.module)}
                  <optgroup label={g.module}>
                    {#each g.policies as p (p.name)}<option value={p.name}>{p.name}</option>{/each}
                  </optgroup>
                {/each}
              </select>
              <Button disabled={!toAdd} onclick={add} icon="plus">{t('settings.roles.addPolicyButton')}</Button>
            </div>

            {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
            <div class="h-row">
              <Button type="submit" variant="primary" loading={form.pending} disabled={!form.dirty}>{t('common.save')}</Button>
              <span class="h-spacer"></span>
              {#if !isNew}<Button variant="danger" icon="trash" onclick={() => (confirmDelete = true)}>{t('common.delete')}</Button>{/if}
            </div>
          </form>
        {:else}
          <div class="h-stack">
            <Table
              caption={t('settings.roles.holders')}
              loading={holders === null}
              rows={holders ?? []}
              rowKey={(r) => r.a.id}
              columns={[
                { key: 'who', label: t('settings.roles.holder'), cell: whoCell },
                { key: 'kind', label: t('settings.roles.kind'), cell: kindCell },
                { key: 'limits', label: t('settings.assign.limits'), cell: limCell },
              ]}>
              {#snippet empty()}<p class="h-muted">{t('settings.roles.noHolders')}</p>{/snippet}
            </Table>
          </div>
        {/if}
      {/snippet}
    </Tabs>
  {/if}
</div>

{#snippet whoCell(r: { a: Schemas['Assignment']; who: string; to: string })}<a use:link href={href(r.to)}>{r.who}</a>{/snippet}
{#snippet kindCell(r: { a: Schemas['Assignment']; who: string; to: string })}<Badge>{r.a.principalKind}</Badge>{/snippet}
{#snippet limCell(r: { a: Schemas['Assignment']; who: string; to: string })}{limitText(r.a.limitations) || t('settings.assign.unlimited')}{/snippet}

<Confirm
  bind:open={confirmDelete}
  title={t('common.delete')}
  message={t('settings.roles.deleteConfirm', { name: role?.name ?? '' })}
  confirm={t('common.delete')}
  onconfirm={remove} />

<style>
  .h-wide {
    max-width: 52rem;
  }
  .h-roles__list {
    list-style: none;
    padding: 0;
  }
  .h-roles__pick {
    max-width: 22rem;
  }
</style>
