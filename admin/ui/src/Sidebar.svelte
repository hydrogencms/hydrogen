<script lang="ts">
  import { session } from './lib/auth/session.svelte';
  import { types, groupTypes } from './lib/types.svelte';
  import { channels } from './lib/channels.svelte';
  import { router, href, link } from './lib/router.svelte';
  import { theme, type Theme } from './lib/theme.svelte';
  import { t } from './lib/i18n';
  import { ext } from './lib/extensions.svelte';
  import { parseViews } from './routes/collection/query';
  import Icon from './lib/ui/Icon.svelte';
  import Logo from './lib/ui/Logo.svelte';
  import Avatar from './lib/ui/Avatar.svelte';
  import Menu, { type MenuItem } from './lib/ui/Menu.svelte';
  import IconButton from './lib/ui/IconButton.svelte';
  import Segmented from './lib/ui/Segmented.svelte';
  import Skeleton from './lib/ui/Skeleton.svelte';
  import Button from './lib/ui/Button.svelte';

  let {
    collapsed,
    open,
    sites,
    loadError,
    ontoggle,
    onclose,
    onretry,
    onsignout,
  }: {
    /** 64 px icon rail (from 768 px up). */
    collapsed: boolean;
    /** The drawer is open (below 768 px). */
    open: boolean;
    sites: string[];
    loadError: boolean;
    ontoggle: () => void;
    /** Closes the drawer (scrim click). */
    onclose: () => void;
    onretry: () => void;
    onsignout: () => void;
  } = $props();

  const typeVisible = (name: string) => channels.showsType(name) && session.can('content.read', { type: name });
  const contentGroups = $derived(groupTypes(types.listable.filter((x) => typeVisible(x.name))));
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

  // Collapsible groups stay closed until the current page is inside them; the rail shows everything.
  let expanded = $state(new Set<string>());
  $effect(() => {
    const g = router.path.startsWith('/settings') ? 'settings' : router.path.startsWith('/singletons') ? 'single' : '';
    if (g && !expanded.has(g)) expanded = new Set([...expanded, g]);
  });
  const isOpen = (g: string) => collapsed || expanded.has(g);
  function toggle(g: string) {
    const next = new Set(expanded);
    if (next.has(g)) next.delete(g);
    else next.add(g);
    expanded = next;
  }

  // Saved views (written by the collection list, per user and type) nest under their collection.
  let tick = $state(0);
  $effect(() => {
    const bump = () => tick++;
    addEventListener('h:views', bump);
    addEventListener('storage', bump);
    return () => {
      removeEventListener('h:views', bump);
      removeEventListener('storage', bump);
    };
  });
  const viewsOf = (type: string) => {
    void tick;
    return parseViews(localStorage.getItem(`h.views.${session.principalKey}.${type}`));
  };
  const viewHref = (type: string, query: string) => `/collections/${type}${query ? `?${query}` : ''}`;
  const activeView = (type: string) =>
    router.path === `/collections/${type}` ? viewsOf(type).find((v) => v.query === router.query.toString()) : undefined;

  const current = (to: string) => (router.path === to || (to !== '/' && router.path.startsWith(to + '/')) ? 'page' : undefined);

  const themes: Array<{ value: Theme; label: string; icon: string }> = $derived([
    { value: 'system', label: t('top.themeSystem'), icon: 'monitor' },
    { value: 'light', label: t('top.themeLight'), icon: 'sun' },
    { value: 'dark', label: t('top.themeDark'), icon: 'moon' },
  ]);
  const themeNow = $derived(themes.find((x) => x.value === theme.value)!);
  const accountMenu = $derived<MenuItem[]>([
    { label: t('nav.account'), icon: 'user', href: '/account' },
    { label: t('top.signOut'), icon: 'logout', separator: true, onselect: onsignout },
  ]);
</script>

{#snippet navlink(to: string, label: string, icon: string, cls = '')}
  <a use:link class="h-nav__link {cls}" href={href(to)} aria-current={current(to)} title={collapsed ? label : undefined}>
    <Icon name={icon} size={18} /><span class="h-nav__label">{label}</span>
  </a>
{/snippet}

{#snippet group(id: string, label: string)}
  <button type="button" class="h-nav__group" aria-expanded={isOpen(id)} onclick={() => toggle(id)}>
    <span>{label}</span><span class="h-nav__chev"><Icon name={isOpen(id) ? 'chevron-up' : 'chevron-down'} size={14} /></span>
  </button>
{/snippet}

<nav class="h-nav" class:h-nav--open={open} aria-label={t('nav.main')}>
  <div class="h-nav__head">
    <a use:link class="h-nav__brand" href={href('/')} title={collapsed ? t('app.name') : undefined}>
      {#if ext.logo}<img class="h-nav__logo" src={ext.logo} alt="" />{:else}<Logo />{/if}<span class="h-nav__label">{t('app.name')}</span>
    </a>
    <IconButton class="h-nav__collapse" icon="panel-left" label={collapsed ? t('nav.expand') : t('nav.collapse')} size="sm" aria-expanded={!collapsed} onclick={ontoggle} />
  </div>

  <div class="h-nav__scroll">
    {@render navlink('/', t('nav.dashboard'), 'dashboard')}

    {#if loadError}
      <p class="h-alert" role="alert">{t('nav.loadFailed')} <Button size="sm" onclick={onretry}>{t('app.retry')}</Button></p>
    {:else if !types.loaded}
      <div class="h-nav__group">{t('nav.content')}</div>
      <Skeleton lines={4} height="1.2rem" />
    {:else}
      {#each contentGroups as g (g.group)}
        <div class="h-nav__group">{g.group || t('nav.content')}</div>
        {#each g.types as ty (ty.name)}
          {@const views = viewsOf(ty.name)}
          {@const av = activeView(ty.name)}
          <a use:link class="h-nav__link" href={href(`/collections/${ty.name}`)} aria-current={av ? undefined : current(`/collections/${ty.name}`)} title={collapsed ? ty.label : undefined}>
            <Icon name={ty.admin?.icon ?? 'file'} size={18} /><span class="h-nav__label">{ty.label}</span>
          </a>
          {#if views.length}
            <ul class="h-nav__sub" aria-label="{ty.label}: {t('nav.views')}">
              {#each views as v (v.name)}
                <li>
                  <a use:link class="h-nav__link" href={href(viewHref(ty.name, v.query))} aria-current={av?.name === v.name ? 'page' : undefined}>
                    <Icon name="star" size={14} /><span class="h-nav__label">{v.name}</span>
                  </a>
                </li>
              {/each}
            </ul>
          {/if}
        {/each}
      {/each}

      {#if singletons.length}
        {@render group('single', t('nav.settingsContent'))}
        {#if isOpen('single')}
          {#each singletons as ty (ty.name)}
            {@render navlink(`/singletons/${ty.name}`, ty.label, ty.admin?.icon ?? 'settings')}
          {/each}
        {/if}
      {/if}

      {#if visibleSites.length}
        <div class="h-nav__group">{t('nav.pages')}</div>
        {#each visibleSites as s (s)}
          {@render navlink(`/pages/${s}`, s, 'sitemap', 'h-nav__link--site')}
        {/each}
      {/if}
    {/if}

    {#each ext.menu() as g (g.menu)}
      <div class="h-nav__group">{g.menu}</div>
      {#each g.views as v (v.path)}
        {@render navlink(v.path, v.title ?? v.path, 'link')}
      {/each}
    {/each}

    {#if settings.length}
      {@render group('settings', t('nav.settings'))}
      {#if isOpen('settings')}
        {#each settings as s (s.to)}
          {@render navlink(s.to, s.label, s.icon)}
        {/each}
      {/if}
    {/if}
  </div>

  <div class="h-nav__foot">
    {#if collapsed}
      <IconButton icon={themeNow.icon} label={t('top.themeCycle', { theme: themeNow.label })} onclick={() => theme.cycle()} />
    {:else}
      <Segmented
        size="sm"
        label={t('top.theme')}
        value={theme.value}
        options={themes.map((x) => ({ value: x.value, label: x.label, icon: x.icon, iconOnly: true }))}
        onchange={(v) => theme.set(v as Theme)}
      />
    {/if}
    <Menu label={t('top.account')} items={accountMenu} align="start" side="top" triggerClass="h-nav__account">
      {#snippet trigger()}
        <Avatar name={session.principal?.name ?? '?'} size={28} />
        <span class="h-sr">{t('top.account')}: {session.principal?.name}</span>
        <span class="h-nav__label" aria-hidden="true">{session.principal?.name}</span>
        <span class="h-nav__chev"><Icon name="chevrons-up-down" size={14} /></span>
      {/snippet}
    </Menu>
  </div>
</nav>
<div class="h-scrim" role="presentation" onclick={onclose}></div>
