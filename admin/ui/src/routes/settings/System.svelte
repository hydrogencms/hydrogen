<script lang="ts">
  import '../../lib/messages/settings';
  import { api } from '../../lib/api/client';
  import { reportError } from '../../lib/errors';
  import { t } from '../../lib/i18n';
  import Button from '../../lib/ui/Button.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import SettingsHead from './SettingsHead.svelte';

  type Report = Awaited<ReturnType<typeof api.get<'/system'>>>;
  type Mod = Report['modules'][number];

  let info = $state<Report | null>(null);
  let failed = $state(false);

  async function load() {
    failed = false;
    try {
      info = await api.get('/system');
    } catch (e) {
      failed = true;
      reportError(e);
    }
  }
  $effect(() => {
    void load();
  });

  const facts = $derived(
    info
      ? [
          { id: 'core', label: t('settings.system.core'), value: info.core.version },
          { id: 'go', label: t('settings.system.go'), value: info.core.go },
          { id: 'database', label: t('settings.system.database'), value: info.database },
          { id: 'search', label: t('settings.system.search'), value: info.search },
          { id: 'media', label: t('settings.system.media'), value: info.media ? t('settings.system.on') : t('settings.system.off') },
        ]
      : [],
  );
</script>

<div class="h-page">
  <SettingsHead section="system" />
  {#if failed}
    <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={load}>{t('app.retry')}</Button></p>
  {:else if !info}
    <Skeleton />
  {:else}
    <dl class="facts" aria-label={t('settings.system.overview')}>
      {#each facts as f (f.id)}
        <div class="fact">
          <dt>{f.label}</dt>
          <dd data-testid={`system-${f.id}`}>{f.value}</dd>
        </div>
      {/each}
    </dl>
    <Table
      caption={t('settings.system.modules')}
      rows={info.modules}
      rowKey={(r: Mod) => r.name}
      columns={[
        { key: 'name', label: t('settings.system.module'), cell: nameCell },
        { key: 'version', label: t('settings.system.version'), cell: versionCell },
        { key: 'module', label: t('settings.system.gomodule'), cell: pathCell, mobile: 'hide' },
        { key: 'requires', label: t('settings.system.requires'), cell: reqCell, mobile: 'hide' },
      ]} />
  {/if}
</div>

{#snippet nameCell(r: Mod)}<strong>{r.name}</strong>{/snippet}
{#snippet versionCell(r: Mod)}<code class="h-mono">{r.version}</code>{/snippet}
{#snippet pathCell(r: Mod)}{#if r.module}<code class="h-mono">{r.module}</code>{:else}<span class="h-muted">{t('settings.system.none')}</span>{/if}{/snippet}
{#snippet reqCell(r: Mod)}{#if r.requires.length}{r.requires.join(', ')}{:else}<span class="h-muted">{t('settings.system.none')}</span>{/if}{/snippet}

<style>
  .facts {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(10rem, 1fr));
    gap: var(--h-space-3);
    margin: 0 0 var(--h-space-4);
  }
  .fact {
    padding: var(--h-space-3);
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius);
  }
  dt {
    font-size: var(--h-text-sm);
    color: var(--h-text-muted);
  }
  dd {
    margin: 2px 0 0;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
</style>
