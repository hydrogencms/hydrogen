<script lang="ts">
  import { api, ApiError, type Schemas } from '../../lib/api/client';
  import { t } from '../../lib/i18n';
  import { href, link } from '../../lib/router.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Card from '../../lib/ui/Card.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Confirm from './Confirm.svelte';
  import LimitationEditor from './LimitationEditor.svelte';
  import { ALL, applicableToRole, formatValues, type LimitDef, type Limits, type PolicyDef } from './limits';
  import { reportError } from '../../lib/errors';
  import { loadCatalog } from './util';
  import { inheritedRows, type Inherited } from './inherited';

  type Assignment = Schemas['Assignment'];
  let {
    kind,
    id,
    bare = false,
    groups = null,
  }: {
    kind: 'user' | 'group' | 'token';
    id: string;
    /** Without the card (inside a dialog). */
    bare?: boolean;
    /** A user's groups: their roles are listed read-only as inherited. */
    groups?: Schemas['Group'][] | null;
  } = $props();

  let rows = $state<Assignment[] | null>(null);
  let failed = $state(false);
  let roles = $state<Schemas['Role'][]>([]);
  let catalog = $state<{ policies: PolicyDef[]; limitations: LimitDef[] }>({ policies: [], limitations: [] });

  let adding = $state(false);
  let roleId = $state('');
  let limits = $state<Limits>({});
  let fieldErrors = $state<Record<string, string | undefined>>({});
  let formError = $state('');
  let saving = $state(false);
  let removing = $state<Assignment | null>(null);
  let confirmOpen = $state(false);

  const role = $derived(roles.find((r) => r.id === roleId));
  const names = $derived(role ? applicableToRole(role.policies, catalog.policies, catalog.limitations) : []);

  async function load() {
    failed = false;
    try {
      rows = (await api.get('/assignments', { query: { principalKind: kind, principalId: id } })).items;
    } catch {
      failed = true;
    }
  }
  $effect(() => {
    void load();
  });

  let inherited = $state<Inherited[]>([]);
  // One request per group; a viewer without role rights simply sees no inherited rows.
  $effect(() => {
    const gs = groups;
    if (!gs) return;
    let stale = false;
    void Promise.all(
      gs.map((g) =>
        api.get('/assignments', { query: { principalKind: 'group', principalId: g.id } }).then(
          (r) => r.items,
          () => [] as Assignment[],
        ),
      ),
    ).then((per) => {
      if (!stale) inherited = inheritedRows(gs, per);
    });
    return () => {
      stale = true;
    };
  });

  async function openAdd() {
    roleId = '';
    limits = {};
    fieldErrors = {};
    formError = '';
    adding = true;
    try {
      const [r, c] = await Promise.all([api.get('/roles', { query: { limit: 500 } }), loadCatalog()]);
      roles = r.items;
      catalog = c;
    } catch (e) {
      adding = false;
      reportError(e);
    }
  }

  async function add() {
    saving = true;
    fieldErrors = {};
    formError = '';
    const body: Limits = {};
    for (const n of names) if (limits[n]?.length) body[n] = limits[n];
    try {
      await api.post('/assignments', { body: { roleId, principalKind: kind, principalId: id, limitations: body } });
      adding = false;
      toast.success(t('settings.assign.added'));
      await load();
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500) throw e;
      if (e.fields?.length) {
        for (const f of e.fields) fieldErrors[f.path.replace(/^limitations\./, '')] = f.message;
        formError = e.message;
      } else if (e.status === 403) reportError(e);
      else formError = e.message;
    } finally {
      saving = false;
    }
  }

  async function remove() {
    if (!removing) return;
    try {
      await api.del('/assignments/{id}', { params: { id: removing.id } });
      toast.success(t('settings.assign.removed'));
      await load();
    } catch (e) {
      reportError(e);
      throw e;
    }
  }

  const limitList = (l: Limits) =>
    Object.keys(l)
      .sort()
      .map((k) => `${k}: ${formatValues(l[k])}`);
</script>

{#snippet content()}
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else}
    <div class="h-assign" class:h-assign--card={!bare}>
      <Table
        caption={t('settings.assign.title')}
        loading={!rows}
        rows={rows ?? []}
        rowKey={(r) => r.id}
        columns={[
          { key: 'role', label: t('settings.assign.role'), cell: roleCell },
          { key: 'limitations', label: t('settings.assign.limits'), cell: limitCell },
          { key: 'actions', label: t('common.actions'), align: 'end', cell: actionCell },
        ]}>
        {#snippet empty()}<p class="h-muted">{inherited.length ? t('settings.assign.noneDirect') : t('settings.assign.empty')}</p>{/snippet}
      </Table>
      {#if inherited.length}
        <h3 class="h-assign__sub">{t('settings.assign.inherited')}</h3>
        <Table
          caption={t('settings.assign.inherited')}
          rows={inherited}
          rowKey={(r) => r.assignment.id}
          columns={[
            { key: 'role', label: t('settings.assign.role'), cell: inhRoleCell },
            { key: 'group', label: t('settings.assign.viaGroup'), cell: inhGroupCell },
            { key: 'limitations', label: t('settings.assign.limits'), cell: inhLimitCell },
          ]} />
      {/if}
    </div>
  {/if}
{/snippet}

{#if bare}
  <div class="h-stack">
    <div class="h-row">
      <h2 id="as-h" class="h-assign__title">{t('settings.assign.title')}</h2>
      <span class="h-spacer"></span>
      <Button icon="plus" onclick={openAdd}>{t('settings.assign.add')}</Button>
    </div>
    {@render content()}
  </div>
{:else}
  <Card title={t('settings.assign.title')} padding="none">
    {#snippet action()}<Button size="sm" icon="plus" onclick={openAdd}>{t('settings.assign.add')}</Button>{/snippet}
    {@render content()}
  </Card>
{/if}

{#snippet roleCell(r: Assignment)}<a use:link href={href(`/settings/roles/${r.roleId}`)}>{r.role ?? r.roleId}</a>{/snippet}
{#snippet limitCell(r: Assignment)}
  {@const l = limitList(r.limitations)}
  {#if l.length === 0}<span class="h-muted">{t('settings.assign.unlimited')}</span>{:else}<span class="h-limits">{#each l as x (x)}<Badge>{x}</Badge>{/each}</span>{/if}
{/snippet}
{#snippet inhRoleCell(r: Inherited)}<a use:link href={href(`/settings/roles/${r.assignment.roleId}`)}>{r.assignment.role ?? r.assignment.roleId}</a>{/snippet}
{#snippet inhGroupCell(r: Inherited)}<a use:link href={href(`/settings/groups/${r.group.id}`)}>{r.group.name}</a>{/snippet}
{#snippet inhLimitCell(r: Inherited)}{@render limitCell(r.assignment)}{/snippet}
{#snippet actionCell(r: Assignment)}
  <IconButton
    icon="trash"
    size="sm"
    label={t('settings.assign.removeRole', { role: r.role ?? r.roleId })}
    onclick={() => {
      removing = r;
      confirmOpen = true;
    }} />
{/snippet}

<Dialog bind:open={adding} title={t('settings.assign.add')} size="md">
  <form
    id="assign-form"
    class="h-stack"
    onsubmit={(e) => {
      e.preventDefault();
      void add();
    }}>
    <Field label={t('settings.assign.role')} required>
      {#snippet children(c)}
        <Select
          {...c}
          bind:value={roleId}
          placeholder={t('settings.assign.pickRole')}
          options={roles.map((r) => ({ value: r.id, label: r.name }))}
          onchange={() => (limits = {})} />
      {/snippet}
    </Field>
    {#if role}
      {#if role.policies.some((p) => p.policy === ALL)}
        <p class="h-muted">{t('settings.assign.noLimitEverything')}</p>
      {:else}
        <h3>{t('settings.assign.limits')}</h3>
        <LimitationEditor {names} defs={catalog.limitations} bind:value={limits} errors={fieldErrors} prefix="as" />
      {/if}
    {/if}
    {#if formError}<p class="h-alert" role="alert">{formError}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={() => (adding = false)}>{t('common.cancel')}</Button>
    <Button type="submit" form="assign-form" variant="primary" loading={saving} disabled={!roleId}>{t('settings.assign.add')}</Button>
  {/snippet}
</Dialog>

<Confirm
  bind:open={confirmOpen}
  title={t('settings.assign.remove')}
  message={t('settings.assign.removeConfirm', { role: removing?.role ?? '' })}
  confirm={t('settings.assign.remove')}
  onconfirm={remove} />

<style>
  .h-assign--card :global(.h-table) {
    border: 0;
    border-top: 1px solid var(--h-border);
    border-radius: 0;
  }
  .h-assign__title {
    font-size: var(--h-text-lg);
  }
  .h-assign__sub {
    margin: 0;
    padding: var(--h-space-4) var(--h-space-4) var(--h-space-2);
    font-size: var(--h-text-sm);
    font-weight: 600;
    color: var(--h-text-muted);
  }
  .h-limits {
    display: flex;
    flex-wrap: wrap;
    gap: var(--h-space-1);
  }
</style>
