<script lang="ts">
  import Badge from '../../lib/ui/Badge.svelte';
  import { t } from '../../lib/i18n';

  let { status }: { status: string } = $props();
  const tone = $derived(status === 'published' ? 'success' : status === 'changed' ? 'warning' : status === 'unpublished' ? 'danger' : 'neutral');
</script>

<Badge {tone} dot>{t(`collections.status.${status}`)}</Badge>
