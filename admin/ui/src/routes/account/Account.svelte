<script lang="ts">
  import '../../lib/messages/settings';
  import { api, ApiError } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { router } from '../../lib/router.svelte';
  import { session } from '../../lib/auth/session.svelte';
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
</script>

<div class="h-page">
  <div class="h-page__head"><h1>{t('account.title')}</h1></div>
  <div class="h-grid">
    <section class="h-card h-stack" aria-labelledby="acc-info">
      <h2 class="h-card__title" id="acc-info">{t('account.info')}</h2>
      <p><strong>{session.principal?.name}</strong> <Badge>{session.principal?.kind}</Badge></p>
      <div>
        <p class="h-muted">{t('account.roles')}</p>
        {#if roles.length === 0}
          <p>{t('common.none')}</p>
        {:else}
          <p class="h-row">{#each roles as r (r)}<Badge tone="info">{r}</Badge>{/each}</p>
        {/if}
      </div>
      <div><Button icon="logout" onclick={() => session.logout()}>{t('account.signOut')}</Button></div>
    </section>

    {#if isUser}
      <section class="h-card" aria-labelledby="acc-pw">
        <h2 class="h-card__title" id="acc-pw">{t('account.changePassword')}</h2>
        <form
          class="h-stack"
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
      </section>
    {/if}
  </div>
</div>
