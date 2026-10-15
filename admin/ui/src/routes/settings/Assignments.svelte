<script lang="ts">
  import { api, ApiError, type Schemas } from '../../lib/api/client';
  import { formatDateTime } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { href, link } from '../../lib/router.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Confirm from './Confirm.svelte';
  import LimitationEditor from './LimitationEditor.svelte';
  import { applicableToRole, formatValues, type LimitDef, type Limits, type PolicyDef } from './limits';
  import { reportError } from '../../lib/errors';
  import { loadCatalog } from './util';

  type Assignment = Schemas['Assignment'];
  let { kind, id }: { kind: 'user' | 'group' | 'token'; id: string } = $props();

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

  const limitText = (l: Limits) =>
    Object.keys(l)
      .sort()
      .map((k) => `${k}: ${formatValues(l[k])}`)
      .join('; ');
</script>

<section class="h-stack" aria-labelledby="as-h">
  <div class="h-row">
    <h2 id="as-h">{t('settings.assign.title')}</h2>
    <span class="h-spacer"></span>
    <Button variant="primary" icon="plus" onclick={openAdd}>{t('settings.assign.add')}</Button>
  </div>
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else if !rows}
    <Skeleton lines={3} height="1.4rem" />
  {:else}
    <Table
      caption={t('settings.assign.title')}
      rows={rows}
      rowKey={(r) => r.id}
      columns={[
        { key: 'role', label: t('settings.assign.role'), cell: roleCell },
        { key: 'limitations', label: t('settings.assign.limits'), cell: limitCell },
        { key: 'createdAt', label: t('settings.created'), cell: createdCell },
        { key: 'actions', label: t('common.actions'), align: 'end', cell: actionCell },
      ]}>
      {#snippet empty()}<p class="h-muted">{t('settings.assign.empty')}</p>{/snippet}
    </Table>
  {/if}
</section>

{#snippet roleCell(r: Assignment)}<a use:link href={href(`/settings/roles/${r.roleId}`)}>{r.role ?? r.roleId}</a>{/snippet}
{#snippet limitCell(r: Assignment)}{limitText(r.limitations) || t('settings.assign.unlimited')}{/snippet}
{#snippet createdCell(r: Assignment)}{formatDateTime(r.createdAt)}{/snippet}
{#snippet actionCell(r: Assignment)}
  <Button
    size="sm"
    variant="danger"
    onclick={() => {
      removing = r;
      confirmOpen = true;
    }}>{t('settings.assign.remove')}</Button>
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
      <h3>{t('settings.assign.limits')}</h3>
      <LimitationEditor {names} defs={catalog.limitations} bind:value={limits} errors={fieldErrors} prefix="as" />
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
