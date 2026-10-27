<script lang="ts">
  import '../../lib/messages/settings';
  import { untrack } from 'svelte';
  import { api, type Schemas } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { href, link, router } from '../../lib/router.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Card from '../../lib/ui/Card.svelte';
  import Chip from '../../lib/ui/Chip.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Popover from '../../lib/ui/Popover.svelte';
  import DetailLayout from './DetailLayout.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Confirm from './Confirm.svelte';
  import LimitationEditor from './LimitationEditor.svelte';
  import {
    ALL, addPolicy, addable, applicableTo, cleanPolicies, formatValues, groupPolicies, policyModule, removePolicy, setLimit,
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
  let editing = $state<Record<string, boolean>>({});
  const form = new Form<{ name: string; description: string; policies: RolePolicy[] }>({ name: '', description: '', policies: [] });
  form.guard();
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
  // The role's policies by module prefix, keeping each policy's index in the form.
  const sections = $derived.by(() => {
    const by = new Map<string, number[]>();
    form.values.policies.forEach((p, i) => by.set(policyModule(p.policy), [...(by.get(policyModule(p.policy)) ?? []), i]));
    return [...by.keys()].sort((a, b) => (a === ALL ? -1 : b === ALL ? 1 : a.localeCompare(b))).map((module) => ({ module, indices: by.get(module)! }));
  });
  const sentence = (m: string) => m.charAt(0).toUpperCase() + m.slice(1);
  const everything = $derived(form.values.policies.some((p) => p.policy === ALL));
  const describe = (name: string) => (name === ALL ? t('settings.roles.everything') : catalog?.policies.find((p) => p.name === name)?.description ?? '');

  function add(policy: string, close: () => void) {
    form.values.policies = addPolicy(form.values.policies, policy);
    close();
  }
  const limitLabels = (l: Record<string, string[]>) =>
    Object.keys(l)
      .filter((k) => l[k].length > 0)
      .sort()
      .map((k) => ({ name: k, text: `${k}: ${formatValues(l[k])}` }));
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
    if (!isNew && id) untrack(() => void loadHolders());
  });
  const overflow = $derived(!isNew && role ? [{ label: t('common.delete'), icon: 'trash', danger: true, onselect: () => (confirmDelete = true) }] : []);
</script>

<div class="h-page">
  <PageHeader
    title={isNew ? t('settings.roles.new') : (role?.name ?? t('nav.roles'))}
    breadcrumbs={[{ label: t('nav.settings') }, { label: t('nav.roles'), href: '/settings/roles' }, { label: isNew ? t('settings.roles.new') : (role?.name ?? '…') }]}
    meta={role?.description || undefined}
    {overflow}>
    {#snippet badge()}{#if role?.template}<Badge tone="info">{t('settings.roles.fromTemplate', { name: role.template })}</Badge>{/if}{/snippet}
    {#snippet primary()}
      {#if catalog}<Button type="submit" form="role-form" variant="primary" loading={form.pending} disabled={!form.dirty}>{t('common.save')}</Button>{/if}
    {/snippet}
  </PageHeader>
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else if !catalog || (!isNew && !role)}
    <DetailLayout>
      {#snippet main()}<Card><Skeleton lines={3} height="1.4rem" /></Card><Card><Skeleton lines={4} height="1.4rem" /></Card>{/snippet}
      {#snippet side()}<Card><Skeleton lines={3} height="1.4rem" /></Card>{/snippet}
    </DetailLayout>
  {:else}
    <DetailLayout>
      {#snippet main()}
        <form
          id="role-form"
          class="h-roles__form"
          onsubmit={(e) => {
            e.preventDefault();
            void save();
          }}>
          <Card title={t('settings.tab.details')}>
            <div class="h-stack h-roles__narrow">
              <Field label={t('settings.roles.name')} error={form.error('name')} required>
                {#snippet children(c)}<Input {...c} bind:value={form.values.name} oninput={() => form.clearError('name')} />{/snippet}
              </Field>
              <Field label={t('settings.description')} error={form.error('description')}>
                {#snippet children(c)}<Input {...c} bind:value={form.values.description} />{/snippet}
              </Field>
            </div>
          </Card>

          <Card title={t('settings.roles.policies')} padding="none" class="h-roles__policies">
            {#snippet action()}
              <Popover label={t('settings.roles.addPolicy')} triggerClass="h-pill">
                {#snippet trigger()}<Icon name="plus" size={16} />{t('settings.roles.addPolicy')}{/snippet}
                {#snippet children(close)}
                  <div class="h-roles__catalog">
                    {#if !everything}
                      <button type="button" class="h-roles__opt" onclick={() => add(ALL, close)}>
                        <strong>{t('settings.roles.everythingName')}</strong>
                        <span class="h-muted">{t('settings.roles.everything')}</span>
                      </button>
                    {/if}
                    {#each groups as g (g.module)}
                      <p class="h-roles__mod">{sentence(g.module)}</p>
                      {#each g.policies as p (p.name)}
                        <button type="button" class="h-roles__opt" onclick={() => add(p.name, close)}>
                          <strong>{p.name}</strong>
                          {#if p.description}<span class="h-muted">{p.description}</span>{/if}
                        </button>
                      {/each}
                    {/each}
                    {#if groups.length === 0 && everything}<p class="h-muted">{t('settings.roles.nothingToAdd')}</p>{/if}
                  </div>
                {/snippet}
              </Popover>
            {/snippet}
            {#if everything}<p class="h-muted h-roles__note">{t('settings.roles.everythingHint')}</p>{/if}
            {#if form.values.policies.length === 0}
              <EmptyState icon="shield" title={t('settings.roles.noPolicies')} description={t('settings.roles.noPoliciesHint')} />
            {/if}
            {#each sections as sec (sec.module)}
              <section class="h-roles__group" aria-label={sec.module === ALL ? t('settings.roles.everythingName') : sentence(sec.module)}>
                <h3 class="h-roles__mod">{sec.module === ALL ? t('settings.roles.everythingName') : sentence(sec.module)}</h3>
                {#each sec.indices as i (form.values.policies[i].policy)}
                  {@const rp = form.values.policies[i]}
                  {@const labels = limitLabels(rp.limitations)}
                  {@const names = applicableTo(rp.policy, catalog!.policies)}
                  {@const errs = limitErrors(i)}
                  {@const open = editing[rp.policy] || Object.values(errs).some(Boolean)}
                  <div class="h-roles__row" data-policy={rp.policy}>
                    <div class="h-roles__main">
                      <div class="h-roles__name">
                        <code>{rp.policy === ALL ? t('settings.roles.everythingName') : rp.policy}</code>
                        {#if describe(rp.policy)}<span class="h-muted">{describe(rp.policy)}</span>{/if}
                      </div>
                      {#if labels.length > 0}
                        <div class="h-roles__chips">
                          {#each labels as l (l.name)}
                            <Chip
                              variant="applied"
                              label={l.text}
                              removeLabel={t('settings.roles.clearLimit', { name: l.name })}
                              onclick={() => (editing[rp.policy] = !open)}
                              onremove={() => (form.values.policies[i].limitations = setLimit(rp.limitations, l.name, []))} />
                          {/each}
                        </div>
                      {/if}
                      <span class="h-row h-roles__ops">
                        {#if names.length > 0}
                          <Button size="sm" variant="ghost" icon="sliders" aria-expanded={open} onclick={() => (editing[rp.policy] = !open)}>{t('settings.roles.limitations')}</Button>
                        {/if}
                        <IconButton
                          icon="trash"
                          size="sm"
                          label={t('settings.roles.removePolicy', { policy: rp.policy })}
                          onclick={() => (form.values.policies = removePolicy(form.values.policies, i))} />
                      </span>
                    </div>
                    {#if rp.policy === ALL}<p class="h-muted">{t(labels.length ? 'settings.roles.everythingLegacy' : 'settings.roles.everythingNoLimit')}</p>{/if}
                    {#if form.error(`policies[${i}].limitations`)}<p class="h-field__error" role="alert">{form.error(`policies[${i}].limitations`)}</p>{/if}
                    {#if form.error(`policies[${i}].policy`)}<p class="h-field__error" role="alert">{form.error(`policies[${i}].policy`)}</p>{/if}
                    {#if open}
                      <div class="h-roles__edit">
                        <LimitationEditor {names} defs={catalog!.limitations} bind:value={form.values.policies[i].limitations} errors={errs} prefix={`p${i}`} />
                      </div>
                    {/if}
                  </div>
                {/each}
              </section>
            {/each}
          </Card>
          {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
        </form>
      {/snippet}
      {#snippet side()}
        {#if !isNew}
          <Card title={t('settings.roles.holders')} padding="sm">
            {#if holders === null}
              <Skeleton lines={3} height="1.4rem" />
            {:else if holders.length === 0}
              <p class="h-muted">{t('settings.roles.noHolders')}</p>
            {:else}
              <ul class="h-list h-plain">
                {#each holders as h (h.a.id)}
                  <li class="h-list__item h-list__item--stack">
                    <span class="h-row">
                      <a use:link href={href(h.to)}>{h.who}</a>
                      <Badge>{h.a.principalKind}</Badge>
                    </span>
                    {#if Object.keys(h.a.limitations).length > 0}
                      <span class="h-roles__chips">
                        {#each limitLabels(h.a.limitations) as l (l.name)}<Badge>{l.text}</Badge>{/each}
                      </span>
                    {/if}
                  </li>
                {/each}
              </ul>
            {/if}
          </Card>
        {:else}
          <Card><p class="h-muted">{t('settings.roles.newHint')}</p></Card>
        {/if}
      {/snippet}
    </DetailLayout>
  {/if}
</div>

<Confirm
  bind:open={confirmDelete}
  title={t('common.delete')}
  message={t('settings.roles.deleteConfirm', { name: role?.name ?? '' })}
  confirm={t('common.delete')}
  onconfirm={remove} />

<style>
  .h-roles__form {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-5);
  }
  .h-roles__narrow {
    max-width: 40rem;
  }
  .h-roles__note {
    padding: 0 var(--h-space-5) var(--h-space-3);
  }
  .h-roles__group {
    display: flex;
    flex-direction: column;
  }
  .h-roles__mod {
    margin: 0;
    padding: var(--h-space-3) var(--h-space-5) var(--h-space-2);
    border-top: 1px solid var(--h-border);
    color: var(--h-text-muted);
    font-size: var(--h-text-sm);
    font-weight: 500;
  }
  .h-roles__row {
    border-top: 1px solid var(--h-border);
  }
  .h-roles__main {
    display: flex;
    align-items: center;
    gap: var(--h-space-4);
    min-height: 3.25rem;
    padding: 0 var(--h-space-4) 0 var(--h-space-5);
  }
  .h-roles__name {
    display: flex;
    flex: 1 1 18rem;
    flex-direction: column;
    min-width: 0;
    padding: var(--h-space-2) 0;
  }
  .h-roles__name code {
    font-size: var(--h-text-sm);
  }
  .h-roles__name .h-muted {
    font-size: var(--h-text-xs);
  }
  .h-roles__ops {
    flex: none;
    gap: var(--h-space-1);
  }
  .h-roles__edit {
    padding: 0 var(--h-space-5) var(--h-space-4);
  }
  .h-roles__row > .h-field__error {
    padding: 0 var(--h-space-5) var(--h-space-2);
  }
  .h-roles__chips {
    display: flex;
    flex: 1 1 12rem;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: var(--h-space-2);
  }
  .h-roles__catalog {
    display: flex;
    flex-direction: column;
    gap: 2px;
    width: min(26rem, 80vw);
    max-height: min(60vh, 28rem);
    overflow: auto;
  }
  .h-roles__catalog .h-roles__mod {
    padding: var(--h-space-2) var(--h-space-2) 0;
    border-top: 0;
  }
  .h-roles__opt {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--h-space-2) var(--h-space-3);
    border: 0;
    border-radius: var(--h-radius);
    background: transparent;
    color: var(--h-text);
    font: inherit;
    font-size: var(--h-text-md);
    text-align: left;
    cursor: pointer;
  }
  .h-roles__opt:hover {
    background: var(--h-surface-2);
  }
  .h-roles__opt .h-muted {
    font-size: var(--h-text-sm);
  }
</style>
