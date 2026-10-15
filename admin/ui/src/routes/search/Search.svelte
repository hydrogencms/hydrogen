<script lang="ts">
  import EmptyState from '../../lib/ui/EmptyState.svelte';
  import { router } from '../../lib/router.svelte';
  import { t } from '../../lib/i18n';

  const q = $derived(router.query.get('q') ?? '');
</script>

<div class="h-page">
  <h1>{t('search.title')}</h1>
  <EmptyState icon="search" title={q ? t('search.results', { q }) : t('search.title')} description={t('search.placeholder')} />
</div>
