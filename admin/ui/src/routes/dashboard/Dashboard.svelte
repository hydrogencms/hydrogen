<script lang="ts">
  import { api, type Schemas } from '../../lib/api/client';
  import { session } from '../../lib/auth/session.svelte';
  import { channels } from '../../lib/channels.svelte';
  import { types } from '../../lib/types.svelte';
  import { href, link } from '../../lib/router.svelte';
  import { formatDateTime } from '../../lib/format';
  import { t } from '../../lib/i18n';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Icon from '../../lib/ui/Icon.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';

  let entries = $state<Schemas['AuditEntry'][] | null>(null);
  let failed = $state(false);

  async function load() {
    failed = false;
    try {
      entries = (await api.get('/audit', { query: { limit: 10 } })).items;
    } catch {
      failed = true;
    }
  }
  $effect(() => {
    if (session.can('audit.read')) void load();
  });

  const links = $derived([
    ...types.listable
      .filter((x) => channels.showsType(x.name) && session.can('content.read', { type: x.name }))
      .map((x) => ({ to: `/collections/${x.name}`, label: x.label, icon: x.admin?.icon ?? 'file' })),
    ...types.singletons
      .filter((x) => session.can('content.read', { type: x.name }))
      .map((x) => ({ to: `/singletons/${x.name}`, label: x.label, icon: x.admin?.icon ?? 'settings' })),
  ]);
</script>

<div class="h-page">
  <h1>{t('dashboard.welcome', { name: session.principal?.name ?? '' })}</h1>
  <div class="h-grid">
    {#if session.can('audit.read')}
      <section class="h-card" aria-labelledby="d-recent">
        <h2 class="h-card__title" id="d-recent">{t('dashboard.recent')}</h2>
        {#if failed}
          <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
        {:else if !entries}
          <Skeleton lines={5} height="1.1rem" />
        {:else if entries.length === 0}
          <p class="h-muted">{t('dashboard.recentEmpty')}</p>
        {:else}
          <ul class="h-list h-plain">
            {#each entries as e (e.id)}
              <li class="h-list__item h-list__item--stack">
                <span><Badge>{e.action}</Badge> {e.subject ?? ''}</span>
                <span class="h-muted">{t('dashboard.by', { actor: e.actorKind })} · {formatDateTime(e.at)}</span>
              </li>
            {/each}
          </ul>
          <p><a use:link href={href('/settings/audit')}>{t('dashboard.viewAll')}</a></p>
        {/if}
      </section>
    {/if}
    <section class="h-card" aria-labelledby="d-quick">
      <h2 class="h-card__title" id="d-quick">{t('dashboard.quick')}</h2>
      {#if !types.loaded}
        <Skeleton lines={4} height="1.1rem" />
      {:else if links.length === 0}
        <p class="h-muted">{t('dashboard.quickEmpty')}</p>
      {:else}
        <ul class="h-list h-plain">
          {#each links as l (l.to)}
            <li class="h-list__item"><a use:link class="h-row" href={href(l.to)}><Icon name={l.icon} size={16} />{l.label}</a></li>
          {/each}
        </ul>
      {/if}
    </section>
  </div>
</div>
