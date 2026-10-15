<script lang="ts">
  import EmptyState from '../lib/ui/EmptyState.svelte';
  import { t } from '../lib/i18n';

  let { title }: { title?: string } = $props();
</script>

<div class="h-page">
  <EmptyState icon="clock" title={title ?? t('app.comingSoon')} description={t('app.comingSoonText')} />
</div>
