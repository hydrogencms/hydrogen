<script lang="ts">
  import EmptyState from '../lib/ui/EmptyState.svelte';
  import { href, link } from '../lib/router.svelte';
  import { t } from '../lib/i18n';
</script>

<div class="h-page">
  <EmptyState icon="search" title={t('app.notFound')} description={t('app.notFoundText')}>
    {#snippet actions()}<a use:link class="h-button h-button--secondary h-button--md" href={href('/')}>{t('nav.dashboard')}</a>{/snippet}
  </EmptyState>
</div>
