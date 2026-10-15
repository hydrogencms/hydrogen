<script lang="ts">
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import { ApiError } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { router } from '../../lib/router.svelte';
  import { safeNext } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import Slot from '../../lib/ext/Slot.svelte';
  import Logo from '../../lib/ui/Logo.svelte';
  import { ext } from '../../lib/extensions.svelte';

  let login = $state('');
  let password = $state('');
  let pending = $state(false);
  let error = $state('');
  let wait = $state(0);
  let timer: ReturnType<typeof setInterval> | undefined;

  $effect(() => () => clearInterval(timer));

  function throttle(seconds: number) {
    wait = seconds;
    clearInterval(timer);
    timer = setInterval(() => {
      wait -= 1;
      if (wait <= 0) {
        clearInterval(timer);
        error = '';
      } else error = t('login.throttled', { seconds: wait });
    }, 1000);
    error = t('login.throttled', { seconds });
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    if (pending || wait > 0) return;
    pending = true;
    error = '';
    try {
      await session.login(login.trim(), password);
      password = '';
      await router.navigate(safeNext(router.query.get('next')), { replace: true, force: true });
    } catch (err) {
      if (err instanceof ApiError && err.status === 429) throttle(err.retryAfter ?? 60);
      else if (err instanceof ApiError && (err.status === 401 || err.status === 400)) error = t('login.failed');
      else error = t('login.error');
    } finally {
      pending = false;
    }
  }
</script>

<main class="h-login">
  <div class="h-login__form">
    <Slot at="login.before" />
    <form onsubmit={submit}>
      {#if ext.logo}<img class="h-login__logo" src={ext.logo} alt="" />{:else}<Logo size={36} />{/if}
      <div>
        <h1>{t('login.heading', { name: t('app.name') })}</h1>
        <p class="h-login__sub">{t('login.sub', { name: t('app.name') })}</p>
      </div>
      {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
      <Field label={t('login.login')} required>
        {#snippet children(c)}<Input {...c} bind:value={login} large autocomplete="username" required autofocus />{/snippet}
      </Field>
      <Field label={t('login.password')} required>
        {#snippet children(c)}<Input {...c} bind:value={password} large type="password" autocomplete="current-password" required />{/snippet}
      </Field>
      <Button type="submit" variant="primary" size="lg" block loading={pending} disabled={wait > 0}>{t('login.submit')}</Button>
    </form>
    <Slot at="login.after" />
  </div>
  <aside class="h-login__panel" aria-hidden="true">
    <div class="h-login__card">
      <Logo size={72} />
      <span class="h-login__brand">{t('app.name')}</span>
      <span class="h-login__tagline">{t('login.tagline')}</span>
    </div>
  </aside>
</main>
