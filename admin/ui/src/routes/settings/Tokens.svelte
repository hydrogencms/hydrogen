<script lang="ts">
  import '../../lib/messages/settings';
  import { api, ApiError, type Schemas } from '../../lib/api/client';
  import { formatDateTime } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import { session } from '../../lib/auth/session.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Checkbox from '../../lib/ui/Checkbox.svelte';
  import DatePicker from '../../lib/ui/DatePicker.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Assignments from './Assignments.svelte';
  import Confirm from './Confirm.svelte';
  import { parseValues, toggleValue } from './limits';
  import SecretBox from './SecretBox.svelte';
  import { reportError } from '../../lib/errors';
  import { PAGE } from './util';

  type Token = Schemas['Token'];
  const page = $derived(Math.max(1, Number(router.query.get('page')) || 1));
  let rows = $state<Token[] | null>(null);
  let total = $state(0);
  let failed = $state(false);

  async function load(p: number) {
    failed = false;
    try {
      const r = await api.get('/tokens', { query: { limit: PAGE, offset: (p - 1) * PAGE } });
      rows = r.items;
      total = r.total;
    } catch {
      failed = true;
    }
  }
  $effect(() => {
    void load(page);
  });

  // Create dialog.
  let creating = $state(false);
  let name = $state('');
  let kind = $state('token');
  let expires = $state('');
  let chosen = $state<string[]>([]);
  let roleText = $state('');
  let roles = $state<Schemas['Role'][] | null>(null);
  let rolesDenied = $state(false);
  let errors = $state<Record<string, string>>({});
  let formError = $state('');
  let saving = $state(false);
  let secret = $state('');

  async function openCreate() {
    name = '';
    kind = 'token';
    expires = '';
    chosen = [];
    roleText = '';
    errors = {};
    formError = '';
    secret = '';
    creating = true;
    try {
      roles = (await api.get('/roles', { query: { limit: 500 } })).items;
      rolesDenied = false;
    } catch (e) {
      rolesDenied = true;
      if (!(e instanceof ApiError && e.status === 403)) reportError(e);
    }
  }
  async function create() {
    saving = true;
    errors = {};
    formError = '';
    try {
      const r = await api.post('/tokens', {
        body: {
          name,
          kind: kind as 'token' | 'agent',
          roles: rolesDenied ? parseValues(roleText) : chosen,
          ...(expires ? { expiresAt: new Date(expires).toISOString() } : {}),
        },
      });
      secret = r.secret;
      await load(page);
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500) throw e;
      for (const f of e.fields ?? []) errors[f.path] ??= f.message;
      if (e.status === 403) reportError(e);
      else formError = e.fields?.length ? '' : e.message;
    } finally {
      saving = false;
    }
  }

  let revoking = $state<Token | null>(null);
  let confirmOpen = $state(false);
  async function revoke() {
    if (!revoking) return;
    try {
      await api.del('/tokens/{id}', { params: { id: revoking.id } });
      toast.success(t('settings.tokens.wasRevoked'));
      await load(page);
    } catch (e) {
      reportError(e);
      throw e;
    }
  }
  let rolesOf = $state<Token | null>(null);
  let rolesOpen = $state(false);

  const tokenState = (tk: Token) =>
    tk.revokedAt ? 'revoked' : tk.expiresAt && new Date(tk.expiresAt).getTime() < Date.now() ? 'expired' : 'active';
</script>

<div class="h-page">
  <div class="h-page__head">
    <h1>{t('nav.tokens')}</h1>
    {#if session.can('token.manage')}<Button variant="primary" icon="plus" onclick={openCreate}>{t('settings.tokens.new')}</Button>{/if}
  </div>
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={() => load(page)}>{t('app.retry')}</Button></p>
  {:else}
    <Table
      caption={t('nav.tokens')}
      loading={!rows}
      rows={rows ?? []}
      rowKey={(r) => r.id}
      columns={[
        { key: 'name', label: t('settings.tokens.name') },
        { key: 'kind', label: t('settings.tokens.kind'), cell: kindCell },
        { key: 'prefix', label: t('settings.tokens.prefix'), cell: prefixCell },
        { key: 'createdAt', label: t('settings.created'), cell: createdCell },
        { key: 'expiresAt', label: t('settings.tokens.expires'), cell: expiresCell },
        { key: 'lastUsedAt', label: t('settings.tokens.lastUsed'), cell: usedCell },
        { key: 'status', label: t('settings.users.status'), cell: statusCell },
        { key: 'actions', label: t('common.actions'), align: 'end', cell: actionCell },
      ]}>
      {#snippet empty()}<p class="h-muted">{t('settings.tokens.empty')}</p>{/snippet}
    </Table>
    <Pagination {page} pageSize={PAGE} {total} onpage={(p) => router.setQuery({ page: p === 1 ? undefined : p }, { push: true })} />
  {/if}
</div>

{#snippet kindCell(r: Token)}<Badge tone={r.kind === 'agent' ? 'info' : 'neutral'}>{r.kind}</Badge>{/snippet}
{#snippet prefixCell(r: Token)}<code>{r.prefix}…</code>{/snippet}
{#snippet createdCell(r: Token)}{formatDateTime(r.createdAt)}{/snippet}
{#snippet expiresCell(r: Token)}{r.expiresAt ? formatDateTime(r.expiresAt) : t('settings.tokens.never')}{/snippet}
{#snippet usedCell(r: Token)}{r.lastUsedAt ? formatDateTime(r.lastUsedAt) : t('settings.users.never')}{/snippet}
{#snippet statusCell(r: Token)}
  {@const s = tokenState(r)}
  <Badge tone={s === 'active' ? 'success' : s === 'revoked' ? 'danger' : 'warning'}>{t('settings.tokens.' + s)}</Badge>
{/snippet}
{#snippet actionCell(r: Token)}
  <span class="h-row">
    <Button
      size="sm"
      onclick={() => {
        rolesOf = r;
        rolesOpen = true;
      }}>{t('nav.roles')}</Button>
    {#if !r.revokedAt}
      <Button
        size="sm"
        variant="danger"
        onclick={() => {
          revoking = r;
          confirmOpen = true;
        }}>{t('settings.tokens.revoke')}</Button>
    {/if}
  </span>
{/snippet}

<Dialog bind:open={creating} title={secret ? t('settings.tokens.createdTitle') : t('settings.tokens.new')} dismissible={!saving && !secret}>
  {#if secret}
    <SecretBox value={secret} warning={t('settings.tokens.secretWarning')} />
  {:else}
    <form
      id="token-new"
      class="h-stack"
      onsubmit={(e) => {
        e.preventDefault();
        void create();
      }}>
      <Field label={t('settings.tokens.name')} error={errors.name} required>
        {#snippet children(c)}<Input {...c} bind:value={name} />{/snippet}
      </Field>
      <Field label={t('settings.tokens.kind')} description={t('settings.tokens.kindHint')} error={errors.kind}>
        {#snippet children(c)}
          <Select {...c} bind:value={kind} options={[{ value: 'token', label: t('settings.tokens.kindToken') }, { value: 'agent', label: t('settings.tokens.kindAgent') }]} />
        {/snippet}
      </Field>
      {#if rolesDenied}
        <Field label={t('settings.tokens.roles')} description={t('settings.tokens.rolesText')} error={errors.roles}>
          {#snippet children(c)}<Input {...c} bind:value={roleText} />{/snippet}
        </Field>
      {:else}
        <fieldset class="h-tokroles">
          <legend>{t('settings.tokens.roles')}</legend>
          {#if roles === null}
            <p class="h-muted">{t('app.loading')}</p>
          {:else if roles.length === 0}
            <p class="h-muted">{t('settings.roles.empty')}</p>
          {/if}
          {#each roles ?? [] as r (r.id)}
            <Checkbox label={r.name} checked={chosen.includes(r.id)} onchange={(e) => (chosen = toggleValue(chosen, r.id, e.currentTarget.checked))} />
          {/each}
          {#if errors.roles}<p class="h-field__error" role="alert">{errors.roles}</p>{/if}
        </fieldset>
      {/if}
      <Field label={t('settings.tokens.expires')} description={t('settings.tokens.expiresHint')} error={errors.expiresAt}>
        {#snippet children(c)}<DatePicker {...c} type="datetime-local" bind:value={expires} />{/snippet}
      </Field>
      {#if formError}<p class="h-alert" role="alert">{formError}</p>{/if}
    </form>
  {/if}
  {#snippet footer()}
    {#if secret}
      <Button
        variant="primary"
        onclick={() => {
          secret = '';
          creating = false;
        }}>{t('common.close')}</Button>
    {:else}
      <Button onclick={() => (creating = false)}>{t('common.cancel')}</Button>
      <Button type="submit" form="token-new" variant="primary" loading={saving}>{t('common.create')}</Button>
    {/if}
  {/snippet}
</Dialog>

<Dialog bind:open={rolesOpen} title={t('settings.tokens.rolesOf', { name: rolesOf?.name ?? '' })} size="lg">
  {#if rolesOf}{#key rolesOf.id}<Assignments kind="token" id={rolesOf.id} />{/key}{/if}
</Dialog>

<Confirm
  bind:open={confirmOpen}
  title={t('settings.tokens.revoke')}
  message={t('settings.tokens.revokeConfirm', { name: revoking?.name ?? '' })}
  confirm={t('settings.tokens.revoke')}
  onconfirm={revoke} />

<style>
  .h-tokroles {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius);
    padding: var(--h-space-3);
  }
  .h-tokroles legend {
    padding: 0 var(--h-space-2);
    font-weight: 600;
  }
</style>
