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
  <Slot at="login.before" />
  <form class="h-login__card" onsubmit={submit}>
    <h1>{t('app.name')}</h1>
    <h2>{t('login.title')}</h2>
    {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
    <Field label={t('login.login')} required>
      {#snippet children(c)}<Input {...c} bind:value={login} autocomplete="username" required autofocus />{/snippet}
    </Field>
    <Field label={t('login.password')} required>
      {#snippet children(c)}<Input {...c} bind:value={password} type="password" autocomplete="current-password" required />{/snippet}
    </Field>
    <Button type="submit" variant="primary" loading={pending} disabled={wait > 0}>{t('login.submit')}</Button>
  </form>
  <Slot at="login.after" />
</main>
