<script lang="ts">
  import '../../lib/messages/settings';
  import { api, ApiError } from '../../lib/api/client';
  import { untrack } from 'svelte';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import { session } from '../../lib/auth/session.svelte';
  import Avatar from '../../lib/ui/Avatar.svelte';
  import Card from '../../lib/ui/Card.svelte';
  import PageHeader from '../../lib/ui/PageHeader.svelte';
  import Segmented from '../../lib/ui/Segmented.svelte';
  import { theme, type Theme } from '../../lib/theme.svelte';
  import { type Schemas } from '../../lib/api/client';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import { submitForm } from '../settings/util';

  const form = new Form({ current: '', next: '', confirm: '' });
  form.guard();
  let mismatch = $state('');

  async function change() {
    mismatch = '';
    if (form.values.next !== form.values.confirm) {
      mismatch = t('account.mismatch');
      return;
    }
    let wrong = '';
    const ok = await submitForm(form, async (v) => {
      try {
        await api.post('/auth/password', { body: { current: v.current, new: v.next } });
      } catch (e) {
        if (e instanceof ApiError && e.code === 'wrong_password') wrong = e.message;
        throw e;
      }
    });
    if (!ok) {
      if (wrong) {
        form.errors = { current: wrong };
        form.formError = '';
      }
      return;
    }
    form.reset({ current: '', next: '', confirm: '' });
    // The server keeps this session and ends the user's other ones.
    toast.success(t('account.changed'));
  }
  // Grants are shown as role names; the same role may grant many policies.
  const roles = $derived([...new Set(session.grants.map((g) => g.role))].sort());
  const isUser = $derived(session.principal?.kind === 'user');

  // Login and email come from the user record; a principal that may not read it just sees the name.
  let me = $state<Schemas['User'] | null>(null);
  $effect(() => {
    const id = session.principal?.id;
    if (!id || !isUser) return;
    untrack(() => {
      api
        .get('/users/{id}', { params: { id } })
        .then((u) => (me = u))
        .catch(() => {});
    });
  });
  // The login is only worth showing when the name is something else.
  const sub = $derived(me ? [me.login !== session.principal?.name ? me.login : '', me.email].filter(Boolean).join(' · ') : '');
  const themes = $derived([
    { value: 'system', label: t('account.theme.system'), icon: 'monitor' },
    { value: 'light', label: t('account.theme.light'), icon: 'sun' },
    { value: 'dark', label: t('account.theme.dark'), icon: 'moon' },
  ]);
</script>

<div class="h-page">
  <PageHeader title={t('account.title')} breadcrumbs={[{ label: t('account.title') }]} meta={t('account.meta')} />
  <div class="h-acct">
    <div class="h-acct__col">
      <Card title={t('account.info')}>
        <div class="h-acct__who">
          <Avatar name={session.principal?.name ?? '?'} size={40} />
          <div class="h-acct__text">
            <strong>{session.principal?.name}</strong>
            {#if sub}<span class="h-muted">{sub}</span>{/if}
          </div>
          <span class="h-spacer"></span>
          <Badge>{session.principal?.kind}</Badge>
        </div>
        <div class="h-acct__roles">
          <p class="h-muted">{t('account.roles')}</p>
          {#if roles.length === 0}
            <p>{t('common.none')}</p>
          {:else}
            <p class="h-row h-acct__badges">{#each roles as r (r)}<Badge tone="info">{r}</Badge>{/each}</p>
          {/if}
        </div>
        <div class="h-acct__foot"><Button icon="log-out" onclick={() => session.logout()}>{t('account.signOut')}</Button></div>
      </Card>

      <Card title={t('account.appearance')}>
        <p class="h-muted h-acct__hint">{t('account.appearanceHint')}</p>
        <Segmented label={t('account.appearance')} options={themes} value={theme.value} onchange={(v) => theme.set(v as Theme)} />
      </Card>
    </div>

    <div class="h-acct__col">
      {#if isUser}
        <Card title={t('account.changePassword')}>
          <form
            class="h-stack h-acct__form"
            onsubmit={(e) => {
              e.preventDefault();
              void change();
            }}>
            <Field label={t('account.current')} error={form.error('current')} required>
              {#snippet children(c)}<Input {...c} type="password" autocomplete="current-password" bind:value={form.values.current} oninput={() => form.clearError('current')} />{/snippet}
            </Field>
            <Field label={t('account.new')} error={form.error('new')} required>
              {#snippet children(c)}<Input {...c} type="password" autocomplete="new-password" bind:value={form.values.next} oninput={() => form.clearError('new')} />{/snippet}
            </Field>
            <Field label={t('account.confirm')} error={mismatch} required>
              {#snippet children(c)}<Input {...c} type="password" autocomplete="new-password" bind:value={form.values.confirm} oninput={() => (mismatch = '')} />{/snippet}
            </Field>
            {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
            <div>
              <Button type="submit" variant="primary" loading={form.pending} disabled={!form.values.current || !form.values.next}>{t('account.changePassword')}</Button>
            </div>
          </form>
        </Card>
      {/if}
      <Card title={t('account.sessions')}>
        <p class="h-muted">{isUser ? t('account.sessionsNote') : t('account.sessionsToken')}</p>
      </Card>
    </div>
  </div>
</div>

<style>
  .h-acct {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--h-space-5);
    align-items: start;
  }
  @media (min-width: 1100px) {
    .h-acct {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  .h-acct__col {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-5);
    min-width: 0;
  }
  .h-acct__who {
    display: flex;
    align-items: center;
    gap: var(--h-space-3);
  }
  .h-acct__text {
    display: flex;
    flex-direction: column;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .h-acct__roles {
    margin-top: var(--h-space-4);
  }
  .h-acct__badges {
    flex-wrap: wrap;
    margin-top: var(--h-space-1);
  }
  .h-acct__foot {
    margin-top: var(--h-space-4);
  }
  .h-acct__hint {
    margin-bottom: var(--h-space-3);
  }
  .h-acct__form {
    max-width: 28rem;
  }
</style>
