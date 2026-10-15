<script lang="ts">
  import { ApiError } from './lib/api/client';
  import { session } from './lib/auth/session.svelte';
  import { reauth } from './lib/reauth.svelte';
  import { router } from './lib/router.svelte';
  import { t } from './lib/i18n';
  import Button from './lib/ui/Button.svelte';
  import Dialog from './lib/ui/Dialog.svelte';
  import Field from './lib/ui/Field.svelte';
  import Input from './lib/ui/Input.svelte';
  import { toast } from './lib/ui/toast.svelte';

  let login = $state('');
  let password = $state('');
  let error = $state('');
  let pending = $state(false);
  let who = '';

  $effect(() => {
    if (reauth.open) {
      who = session.principal?.name ?? '';
      login = who;
      password = '';
      error = '';
    }
  });

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    pending = true;
    error = '';
    try {
      await session.login(login.trim(), password);
      reauth.open = false;
      if (login.trim() !== who) await router.navigate('/', { force: true });
      else toast.success(t('relogin.done'));
    } catch (err) {
      error = err instanceof ApiError && err.status === 429 ? t('login.throttled', { seconds: err.retryAfter ?? 60 }) : err instanceof ApiError && err.status < 500 ? t('login.failed') : t('login.error');
    } finally {
      pending = false;
    }
  }

  async function signOut() {
    reauth.open = false;
    session.clear();
    await router.navigate('/login', { force: true });
  }
</script>

<Dialog bind:open={reauth.open} title={t('relogin.title')} size="sm" dismissible={false}>
  <form id="relogin" class="h-stack" onsubmit={submit}>
    <p>{t('relogin.text')}</p>
    {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
    <Field label={t('login.login')} required>
      {#snippet children(c)}<Input {...c} bind:value={login} autocomplete="username" required />{/snippet}
    </Field>
    <Field label={t('login.password')} required>
      {#snippet children(c)}<Input {...c} bind:value={password} type="password" autocomplete="current-password" required />{/snippet}
    </Field>
  </form>
  {#snippet footer()}
    <Button onclick={signOut}>{t('top.signOut')}</Button>
    <Button type="submit" form="relogin" variant="primary" loading={pending}>{t('login.submit')}</Button>
  {/snippet}
</Dialog>
