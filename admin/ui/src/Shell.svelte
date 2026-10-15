<script lang="ts">
  import { onMount, type Component, type Snippet } from 'svelte';
  import { api } from './lib/api/client';
  import { config } from './lib/config';
  import { session } from './lib/auth/session.svelte';
  import { types } from './lib/types.svelte';
  import { channels } from './lib/channels.svelte';
  import { router } from './lib/router.svelte';
  import { t } from './lib/i18n';
  import Sidebar from './Sidebar.svelte';
  import Topbar from './Topbar.svelte';

  let { children }: { children: Snippet } = $props();

  let navOpen = $state(false);
  let loadError = $state(false);
  let sites = $state<string[]>([]);
  let scrolled = $state(false);

  async function boot() {
    loadError = false;
    try {
      await Promise.all([types.load(), channels.load()]);
      if (config().pages && session.can('pages.read')) {
        sites = (await api.get('/pages/sites')).items;
      }
    } catch {
      loadError = true;
    }
  }
  onMount(() => void boot());

  // Rail state: remembered per user; without a choice, the sidebar starts as a rail below 1024 px.
  const navKey = () => `h.nav.${session.principalKey}`;
  const small = typeof matchMedia === 'function' ? matchMedia('(max-width: 1023px)') : undefined;
  const stored = localStorage.getItem(navKey());
  let collapsed = $state(stored === null ? !!small?.matches : stored === '1');
  function toggleNav() {
    collapsed = !collapsed;
    localStorage.setItem(navKey(), collapsed ? '1' : '0');
  }
  $effect(() => {
    if (!small) return;
    const on = () => localStorage.getItem(navKey()) === null && (collapsed = small.matches);
    small.addEventListener('change', on);
    return () => small.removeEventListener('change', on);
  });

  // Close the mobile drawer after navigating, and on Escape.
  $effect(() => {
    void router.path;
    navOpen = false;
  });

  $effect(() => {
    const on = () => (scrolled = window.scrollY > 4);
    on();
    addEventListener('scroll', on, { passive: true });
    return () => removeEventListener('scroll', on);
  });

  // Command palette: ⌘K / Ctrl+K; the dialog loads on first use.
  let palette = $state.raw<Component<{ open?: boolean; sites?: string[] }> | null>(null);
  let paletteOpen = $state(false);
  async function openPalette() {
    palette ??= (await import('./CommandPalette.svelte')).default as never;
    paletteOpen = true;
  }
  function keydown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      void openPalette();
    } else if (e.key === 'Escape' && navOpen) navOpen = false;
  }

  async function signOut() {
    try {
      await session.logout();
    } finally {
      await router.navigate('/login', { force: true });
    }
  }
</script>

<svelte:window onkeydown={keydown} />

<div class="h-app" class:h-app--rail={collapsed}>
  <a class="h-skip" href="#main">{t('app.skip')}</a>

  <Sidebar {collapsed} open={navOpen} {sites} {loadError} ontoggle={toggleNav} onclose={() => (navOpen = false)} onretry={boot} onsignout={signOut} />

  <div class="h-shell">
    <Topbar {scrolled} {navOpen} onmenu={() => (navOpen = !navOpen)} onsearch={openPalette} onsignout={signOut} />
    <main class="h-main" id="main" tabindex="-1">{@render children()}</main>
  </div>
</div>

{#if palette}
  {@const Palette = palette}
  <Palette bind:open={paletteOpen} {sites} />
{/if}
