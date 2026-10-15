<script lang="ts">
  import { api } from './lib/api/client';
  import { session } from './lib/auth/session.svelte';
  import { types } from './lib/types.svelte';
  import { channels } from './lib/channels.svelte';
  import { router } from './lib/router.svelte';
  import { t } from './lib/i18n';
  import Dialog from './lib/ui/Dialog.svelte';
  import Icon from './lib/ui/Icon.svelte';

  let { open = $bindable(false), sites = [] }: { open?: boolean; sites?: string[] } = $props();

  interface Cmd {
    id: string;
    group: 'search' | 'recent' | 'items' | 'types' | 'actions';
    label: string;
    hint?: string;
    icon: string;
    run: () => void;
  }
  interface Hit {
    id: string;
    name: string;
    type: string;
  }

  const uid = $props.id();
  let q = $state('');
  let active = $state(0);
  let hits = $state<Hit[]>([]);
  let busy = $state(false);
  let ctl: AbortController | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const recentKey = $derived(`h.recent.${session.principalKey}`);
  const readRecent = (): Hit[] => {
    try {
      const v = JSON.parse(localStorage.getItem(recentKey) ?? '[]');
      return Array.isArray(v) ? v.filter((x) => typeof x?.id === 'string' && typeof x?.name === 'string').slice(0, 5) : [];
    } catch {
      return [];
    }
  };
  let recent = $state<Hit[]>([]);

  const readable = $derived(types.listable.filter((x) => channels.showsType(x.name) && session.can('content.read', { type: x.name })));
  const typeLabel = (name: string) => types.get(name)?.label ?? name;

  function go(path: string) {
    open = false;
    void router.navigate(path);
  }
  function openItem(h: Hit) {
    localStorage.setItem(recentKey, JSON.stringify([h, ...readRecent().filter((r) => r.id !== h.id)].slice(0, 5)));
    go(`/items/${h.id}`);
  }

  const destinations = $derived<Cmd[]>([
    ...readable.map((ty) => ({ id: `t-${ty.name}`, group: 'types' as const, label: ty.label, icon: ty.admin?.icon ?? 'file', run: () => go(`/collections/${ty.name}`) })),
    ...types.singletons
      .filter((x) => session.can('content.read', { type: x.name }))
      .map((ty) => ({ id: `s-${ty.name}`, group: 'types' as const, label: ty.label, icon: ty.admin?.icon ?? 'settings', run: () => go(`/singletons/${ty.name}`) })),
    ...sites.map((s) => ({ id: `p-${s}`, group: 'types' as const, label: s, hint: t('nav.pages'), icon: 'sitemap', run: () => go(`/pages/${s}`) })),
  ]);
  const settingsTo = $derived(
    session.can('user.manage')
      ? '/settings/users'
      : session.can('role.manage')
        ? '/settings/roles'
        : session.can('token.manage')
          ? '/settings/tokens'
          : session.can('audit.read')
            ? '/settings/audit'
            : '',
  );
  const actions = $derived<Cmd[]>([
    ...readable
      .filter((ty) => session.can('content.create', { type: ty.name }))
      .map((ty) => ({
        id: `n-${ty.name}`,
        group: 'actions' as const,
        label: t('palette.new', { type: ty.label }),
        icon: 'plus',
        run: () => go(`/collections/${ty.name}/new${channels.languages?.[0] ? `?locale=${channels.languages[0]}` : ''}`),
      })),
    { id: 'a-dash', group: 'actions', label: t('palette.dashboard'), icon: 'dashboard', run: () => go('/') },
    ...(settingsTo ? [{ id: 'a-set', group: 'actions' as const, label: t('palette.settings'), icon: 'settings', run: () => go(settingsTo) }] : []),
    { id: 'a-acc', group: 'actions', label: t('palette.account'), icon: 'user', run: () => go('/account') },
  ]);

  const term = $derived(q.trim().toLowerCase());
  const match = (c: Cmd) => !term || c.label.toLowerCase().includes(term);

  const flat = $derived.by<Cmd[]>(() => {
    const out: Cmd[] = [];
    if (term) out.push({ id: 'q', group: 'search', label: t('palette.search', { q: q.trim() }), icon: 'search', run: () => go(`/search?q=${encodeURIComponent(q.trim())}`) });
    const asCmd = (h: Hit, group: 'recent' | 'items'): Cmd => ({ id: `i-${h.id}`, group, label: h.name, hint: typeLabel(h.type), icon: types.get(h.type)?.admin?.icon ?? 'file', run: () => openItem(h) });
    if (!term) out.push(...recent.map((h) => asCmd(h, 'recent')));
    else out.push(...hits.map((h) => asCmd(h, 'items')));
    out.push(...destinations.filter(match).slice(0, 6), ...actions.filter(match).slice(0, 8));
    return out;
  });
  const heading = (g: Cmd['group']) => (g === 'search' ? '' : t(`palette.${g}`));

  $effect(() => {
    if (open) {
      q = '';
      active = 0;
      hits = [];
      recent = readRecent();
    }
  });
  $effect(() => {
    void flat.length;
    if (active >= flat.length) active = Math.max(0, flat.length - 1);
  });

  // Items by name across the collections the principal can read: one request per type, newest first.
  $effect(() => {
    const text = q.trim();
    clearTimeout(timer);
    ctl?.abort();
    if (!open || text.length < 2) {
      hits = [];
      busy = false;
      return;
    }
    busy = true;
    const names = readable.slice(0, 10).map((x) => x.name);
    timer = setTimeout(async () => {
      const c = (ctl = new AbortController());
      const lists = await Promise.all(
        names.map((type) =>
          api
            .get('/collections/{type}/items', { params: { type }, query: { q: text, limit: 4 }, signal: c.signal })
            .then((r) => r.items.map((i) => ({ id: i.id, name: i.name || i.id, type })))
            .catch(() => [] as Hit[]),
        ),
      );
      if (c.signal.aborted) return;
      hits = lists.flat().slice(0, 8);
      busy = false;
    }, 200);
    return () => {
      clearTimeout(timer);
      ctl?.abort();
    };
  });

  function key(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (flat.length) active = (active + (e.key === 'ArrowDown' ? 1 : -1) + flat.length) % flat.length;
    } else if (e.key === 'Enter') {
      e.preventDefault();
      flat[active]?.run();
    }
  }
  $effect(() => {
    if (open) document.getElementById(`${uid}-o${active}`)?.scrollIntoView({ block: 'nearest' });
  });
</script>

<Dialog bind:open title={t('palette.title')} bare>
  <div class="h-palette">
    <div class="h-palette__field">
      <Icon name="search" size={18} />
      <input
        class="h-palette__input"
        type="text"
        role="combobox"
        autocomplete="off"
        aria-label={t('top.search')}
        aria-expanded="true"
        aria-controls="{uid}-l"
        aria-activedescendant={flat.length ? `${uid}-o${active}` : undefined}
        placeholder={t('palette.placeholder')}
        bind:value={q}
        onkeydown={key}
      />
    </div>
    <ul class="h-palette__list" id="{uid}-l" role="listbox" aria-label={t('palette.title')}>
      {#each flat as c, i (c.id)}
        {#if i === 0 || flat[i - 1]!.group !== c.group}
          {#if heading(c.group)}<li class="h-palette__group" role="presentation">{heading(c.group)}</li>{/if}
        {/if}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <li id="{uid}-o{i}" role="option" aria-selected={i === active} class="h-palette__item" class:h-palette__item--active={i === active} onpointermove={() => (active = i)} onclick={() => c.run()}>
          <Icon name={c.icon} size={16} /><span>{c.label}</span>{#if c.hint}<span class="h-palette__hint">{c.hint}</span>{/if}
        </li>
      {/each}
      {#if term && busy && hits.length === 0}<li class="h-palette__note" role="presentation">{t('combobox.searching')}</li>{/if}
    </ul>
  </div>
</Dialog>
