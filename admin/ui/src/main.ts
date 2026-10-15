import { mount } from 'svelte';
import './styles/tokens.css';
import './styles/base.css';
import './styles/components.css';
import App from './App.svelte';
import { loadConfig } from './lib/config';
import { ApiError, setHandlers } from './lib/api/client';
import { session } from './lib/auth/session.svelte';
import { router } from './lib/router.svelte';
import { reauth } from './lib/reauth.svelte';
import { theme } from './lib/theme.svelte';
import { toast } from './lib/ui/toast.svelte';
import { t } from './lib/i18n';
import { routes, notFound } from './routes';

const target = document.getElementById('app')!;

async function main() {
  theme.init();
  await loadConfig();

  setHandlers({
    csrf: () => session.csrf,
    unauthorized() {
      const was = session.principal;
      if (was && router.anyDirty) {
        // Keep the page and its unsaved work: ask for the password again instead of navigating.
        reauth.open = true;
        return;
      }
      session.clear();
      if (was) {
        const next = router.path + (location.search ?? '');
        void router.navigate(`/login?next=${encodeURIComponent(next)}`, { replace: true, force: true });
      }
    },
    serverError(e: ApiError) {
      toast.error(e.status ? t('app.serverError', { status: e.status }) : t('app.networkError'));
    },
  });

  router.define(routes, {
    notFound,
    guard(route, path) {
      if (route?.public) return session.principal ? '/' : undefined;
      if (!session.principal) return path === '/' ? '/login' : `/login?next=${encodeURIComponent(path + location.search)}`;
    },
  });

  await session.load();
  mount(App, { target });
  await router.start();
}

main().catch((e) => {
  target.innerHTML = '';
  const p = document.createElement('p');
  p.setAttribute('role', 'alert');
  p.textContent = `${t('app.bootFailed')}: ${e instanceof Error ? e.message : e}`;
  target.append(p);
});
