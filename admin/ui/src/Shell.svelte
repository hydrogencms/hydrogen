<script lang="ts">
  import { onMount, type Snippet } from 'svelte';
  import { api } from './lib/api/client';
  import { config } from './lib/config';
  import { session } from './lib/auth/session.svelte';
  import { types, groupTypes } from './lib/types.svelte';
  import { channels } from './lib/channels.svelte';
  import { router, href, link } from './lib/router.svelte';
  import { theme } from './lib/theme.svelte';
  import { t } from './lib/i18n';
  import Icon from './lib/ui/Icon.svelte';
  import Menu, { type MenuItem } from './lib/ui/Menu.svelte';
  import Select from './lib/ui/Select.svelte';
  import Input from './lib/ui/Input.svelte';
  import IconButton from './lib/ui/IconButton.svelte';
  import Skeleton from './lib/ui/Skeleton.svelte';
  import Button from './lib/ui/Button.svelte';
  import { ext } from './lib/extensions.svelte';

  let { children }: { children: Snippet } = $props();

  let navOpen = $state(false);
  let loadError = $state(false);
  let sites = $state<string[]>([]);
  let q = $state('');

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

  // Close the mobile drawer after navigating.
  $effect(() => {
    void router.path;
    navOpen = false;
  });

  const typeVisible = (name: string) => channels.showsType(name) && session.can('content.read', { type: name });
  const contentGroups = $derived(
    groupTypes(types.listable.filter((x) => typeVisible(x.name))),
  );
  const singletons = $derived(types.singletons.filter((x) => typeVisible(x.name)));
  const visibleSites = $derived(channels.site ? sites.filter((s) => s === channels.site) : sites);
  const settings = $derived(
    [
      { to: '/settings/users', label: t('nav.users'), icon: 'users', ok: session.can('user.manage') },
      { to: '/settings/groups', label: t('nav.groups'), icon: 'layers', ok: session.can('user.manage') },
      { to: '/settings/roles', label: t('nav.roles'), icon: 'shield', ok: session.can('role.manage') },
      { to: '/settings/tokens', label: t('nav.tokens'), icon: 'key', ok: session.can('token.manage') },
      { to: '/settings/audit', label: t('nav.audit'), icon: 'history', ok: session.can('audit.read') },
    ].filter((x) => x.ok),
  );

  const current = (to: string) => (router.path === to || (to !== '/' && router.path.startsWith(to + '/')) ? 'page' : undefined);

  const menu = $derived<MenuItem[]>([
    { label: t('nav.account'), icon: 'user', href: '/account' },
    { label: t('top.themeSystem'), checked: theme.value === 'system', onselect: () => theme.set('system'), separator: true },
    { label: t('top.themeLight'), checked: theme.value === 'light', onselect: () => theme.set('light') },
    { label: t('top.themeDark'), checked: theme.value === 'dark', onselect: () => theme.set('dark') },
    { label: t('top.signOut'), icon: 'logout', separator: true, onselect: signOut },
  ]);

  async function signOut() {
    try {
      await session.logout();
    } finally {
      await router.navigate('/login', { force: true });
    }
  }

  function search(e: SubmitEvent) {
    e.preventDefault();
    const v = q.trim();
    if (v) void router.navigate(`/search?q=${encodeURIComponent(v)}`);
  }
</script>

<div class="h-app">
  <a class="h-skip" href="#main">{t('app.skip')}</a>

  <nav class="h-nav" class:h-nav--open={navOpen} aria-label={t('nav.main')}>
    <a use:link class="h-nav__brand" href={href('/')}>
      {#if ext.logo}<img class="h-nav__logo" src={ext.logo} alt="" />{:else}<Icon name="star" size={20} />{/if}{t('app.name')}
    </a>

    <a use:link class="h-nav__link" href={href('/')} aria-current={current('/') && router.path === '/' ? 'page' : undefined}>
      <Icon name="home" size={16} />{t('nav.dashboard')}
    </a>

    {#if loadError}
      <p class="h-alert" role="alert">{t('nav.loadFailed')} <Button size="sm" onclick={boot}>{t('app.retry')}</Button></p>
    {:else if !types.loaded}
      <div class="h-nav__group">{t('nav.content')}</div>
      <Skeleton lines={4} height="1.2rem" />
    {:else}
      {#each contentGroups as g (g.group)}
        <div class="h-nav__group">{g.group || t('nav.content')}</div>
        {#each g.types as ty (ty.name)}
          <a use:link class="h-nav__link" href={href(`/collections/${ty.name}`)} aria-current={current(`/collections/${ty.name}`)}>
            <Icon name={ty.admin?.icon ?? 'file'} size={16} />{ty.label}
          </a>
        {/each}
      {/each}

      {#if singletons.length}
        <div class="h-nav__group">{t('nav.settingsContent')}</div>
        {#each singletons as ty (ty.name)}
          <a use:link class="h-nav__link" href={href(`/singletons/${ty.name}`)} aria-current={current(`/singletons/${ty.name}`)}>
            <Icon name={ty.admin?.icon ?? 'settings'} size={16} />{ty.label}
          </a>
        {/each}
      {/if}

      {#if visibleSites.length}
        <div class="h-nav__group">{t('nav.pages')}</div>
        {#each visibleSites as s (s)}
          <a use:link class="h-nav__link" href={href(`/pages/${s}`)} aria-current={current(`/pages/${s}`)}>
            <Icon name="page" size={16} />{s}
          </a>
        {/each}
      {/if}
    {/if}

    {#if settings.length}
      <div class="h-nav__group">{t('nav.settings')}</div>
      {#each settings as s (s.to)}
        <a use:link class="h-nav__link" href={href(s.to)} aria-current={current(s.to)}><Icon name={s.icon} size={16} />{s.label}</a>
      {/each}
    {/if}

    {#each ext.menu() as g (g.menu)}
      <div class="h-nav__group">{g.menu}</div>
      {#each g.views as v (v.path)}
        <a use:link class="h-nav__link" href={href(v.path)} aria-current={current(v.path)}><Icon name="link" size={16} />{v.title}</a>
      {/each}
    {/each}

    <div class="h-nav__group">{t('nav.account')}</div>
    <a use:link class="h-nav__link" href={href('/account')} aria-current={current('/account')}><Icon name="user" size={16} />{t('nav.account')}</a>
  </nav>

  <header class="h-topbar">
    <IconButton class="h-topbar__navtoggle" icon="menu" label={t('top.menu')} onclick={() => (navOpen = !navOpen)} aria-expanded={navOpen} />
    {#if channels.items.length}
      <Select
        class="h-topbar__channel"
        aria-label={t('top.channel')}
        value={channels.current}
        placeholder={t('top.allChannels')}
        options={channels.items.filter((c) => session.can('content.read', { channel: c.name })).map((c) => ({ value: c.name, label: c.name }))}
        onchange={(e) => channels.select(e.currentTarget.value)}
      />
    {/if}
    <form class="h-topbar__search" role="search" onsubmit={search}>
      <Input type="search" bind:value={q} aria-label={t('top.search')} placeholder={t('top.searchPlaceholder')} />
    </form>
    <span class="h-spacer"></span>
    <Menu label={t('top.account')} items={menu}>
      {#snippet trigger()}<Icon name="user" size={16} /><span class="h-sr">{t('top.account')}: </span><span>{session.principal?.name}</span><Icon name="chevron-down" size={14} />{/snippet}
    </Menu>
  </header>

  <main class="h-main" id="main" tabindex="-1">{@render children()}</main>
</div>
