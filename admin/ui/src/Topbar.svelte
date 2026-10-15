<script lang="ts">
  import { session } from './lib/auth/session.svelte';
  import { types, groupTypes } from './lib/types.svelte';
  import { channels } from './lib/channels.svelte';
  import { crumbs } from './lib/crumbs.svelte';
  import { router } from './lib/router.svelte';
  import { t } from './lib/i18n';
  import Icon from './lib/ui/Icon.svelte';
  import Avatar from './lib/ui/Avatar.svelte';
  import IconButton from './lib/ui/IconButton.svelte';
  import Breadcrumbs from './lib/ui/Breadcrumbs.svelte';
  import Menu, { type MenuItem } from './lib/ui/Menu.svelte';

  let {
    scrolled,
    navOpen,
    onmenu,
    onsearch,
    onsignout,
  }: {
    scrolled: boolean;
    navOpen: boolean;
    onmenu: () => void;
    onsearch: () => void;
    onsignout: () => void;
  } = $props();

  const mac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.userAgent);

  // The trail a screen declared; otherwise the route's own title.
  const trail = $derived(
    crumbs.items.length ? crumbs.items : router.route?.title ? [{ label: t(router.route.title) }] : [],
  );

  const channelItems = $derived<MenuItem[]>([
    { label: t('top.allChannels'), checked: channels.current === '', onselect: () => channels.select('') },
    ...channels.items
      .filter((c) => session.can('content.read', { channel: c.name }))
      .map((c) => ({
        label: c.name,
        hint: c.languages?.join(', '),
        checked: channels.current === c.name,
        onselect: () => channels.select(c.name),
      })),
  ]);

  const creatable = $derived(
    groupTypes(types.listable.filter((x) => channels.showsType(x.name) && session.can('content.create', { type: x.name }))),
  );
  const createItems = $derived<MenuItem[]>(
    creatable.flatMap((g) => [
      ...(creatable.length > 1 ? [{ label: g.group || t('nav.content'), heading: true }] : []),
      ...g.types.map((ty) => ({ label: ty.label, icon: ty.admin?.icon ?? 'file', href: `/collections/${ty.name}/new${channels.languages?.[0] ? `?locale=${channels.languages[0]}` : ''}` })),
    ]),
  );
  const accountMenu = $derived<MenuItem[]>([
    { label: t('nav.account'), icon: 'user', href: '/account' },
    { label: t('top.signOut'), icon: 'logout', separator: true, onselect: onsignout },
  ]);
</script>

<header class="h-topbar" class:h-topbar--scrolled={scrolled}>
  <IconButton class="h-topbar__navtoggle" icon="menu" label={t('top.menu')} onclick={onmenu} aria-expanded={navOpen} />
  <div class="h-topbar__crumbs">
    {#if trail.length > 1}<Breadcrumbs items={trail} inline />{/if}
  </div>

  <button type="button" class="h-search" aria-label={t('top.openSearch')} aria-keyshortcuts="Control+K Meta+K" onclick={onsearch}>
    <Icon name="search" size={16} /><span>{t('top.searchHint')}</span><kbd class="h-kbd">{mac ? '⌘K' : 'Ctrl K'}</kbd>
  </button>

  {#if channels.items.length}
    <Menu label={t('top.channel')} items={channelItems} triggerClass="h-pill">
      {#snippet trigger()}
        <Icon name="globe" size={16} /><span class="h-pill__label">{channels.current || t('top.allChannels')}</span><Icon name="chevron-down" size={14} />
      {/snippet}
    </Menu>
  {/if}

  {#if createItems.length}
    <Menu label={t('top.create')} items={createItems} triggerClass="h-pill h-pill--primary">
      {#snippet trigger()}<Icon name="plus" size={16} /><span class="h-pill__label">{t('top.create')}</span>{/snippet}
    </Menu>
  {/if}

  <Menu label={t('top.profile')} items={accountMenu} triggerClass="h-topbar__avatar">
    {#snippet trigger()}<Avatar name={session.principal?.name ?? '?'} size={32} /><span class="h-sr">{t('top.profile')}</span>{/snippet}
  </Menu>
</header>
