<script lang="ts">
  import { session } from '../auth/session.svelte';
  import { ext, navigateTo } from '../extensions.svelte';
  import { t } from '../i18n';
  import { router } from '../router.svelte';
  import EmptyState from '../ui/EmptyState.svelte';
  import Extension from './Extension.svelte';

  let { params }: { params: Record<string, string> } = $props();

  const view = $derived(ext.m.views.find((v) => v.path === router.route?.path));
  const ok = $derived(!!view && ext.allowed(view) && !!session.principal);
</script>

<div class="h-page">
  {#if view && ok}
    <h1>{view.title}</h1>
    <Extension
      tag={view.tag}
      props={{ params, query: Object.fromEntries(router.query) }}
      onEvents={{ navigate: (e) => navigateTo(e.detail?.path) }}
    />
  {:else}
    <EmptyState icon="shield" title={t('app.forbidden')} />
  {/if}
</div>
