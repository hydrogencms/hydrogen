<script lang="ts">
  import { onMount } from 'svelte';
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

  // First run: no user exists yet. The server prints a link ending in #setup=<token>; with it this page creates the
  // first administrator instead of signing in, and without it it says where to find the link.
  const tokenInHash = () => new URLSearchParams(location.hash.slice(1)).get('setup') ?? '';
  let setupToken = tokenInHash();
  let required = false;
  let mode = $state<'login' | 'setup' | 'needed'>('login');
  const decide = () => {
    if (required) mode = setupToken ? 'setup' : 'needed';
  };
  onMount(() => {
    session.setupRequired().then(
      (r) => {
        required = r;
        decide();
      },
      () => {},
    );
    // the link opened in a tab already showing this page changes only the fragment
    const onHash = () => {
      setupToken = tokenInHash();
      decide();
    };
    addEventListener('hashchange', onHash);
    return () => removeEventListener('hashchange', onHash);
  });

  async function submitSetup() {
    try {
      await session.setup(setupToken, login.trim(), password);
      password = '';
      history.replaceState(history.state, '', location.pathname + location.search);
      await router.navigate('/', { replace: true, force: true });
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) error = t('setup.invalid');
      else if (err instanceof ApiError && err.status === 409) {
        mode = 'login';
        error = t('setup.done');
      } else if (err instanceof ApiError && err.status === 400) error = err.message || t('setup.error');
      else error = t('setup.error');
    }
  }

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
    if (mode === 'setup') {
      await submitSetup();
      pending = false;
      return;
    }
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
        {#if mode === 'setup'}
          <h1>{t('setup.heading')}</h1>
          <p class="h-login__sub">{t('setup.sub')}</p>
        {:else}
          <h1>{t('login.heading', { name: t('app.name') })}</h1>
          <p class="h-login__sub">{t('login.sub', { name: t('app.name') })}</p>
        {/if}
      </div>
      {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
      {#if mode === 'needed'}<p class="h-alert h-alert--info" role="status" data-testid="setup-needed">{t('setup.needed')}</p>{/if}
      <Field label={t('login.login')} required>
        {#snippet children(c)}<Input {...c} bind:value={login} large autocomplete="username" required autofocus />{/snippet}
      </Field>
      <Field label={mode === 'setup' ? t('setup.password') : t('login.password')} required>
        {#snippet children(c)}<Input {...c} bind:value={password} large type="password" autocomplete={mode === 'setup' ? 'new-password' : 'current-password'} minlength={mode === 'setup' ? 12 : undefined} required />{/snippet}
      </Field>
      <Button type="submit" variant="primary" size="lg" block loading={pending} disabled={wait > 0}>{mode === 'setup' ? t('setup.submit') : t('login.submit')}</Button>
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
