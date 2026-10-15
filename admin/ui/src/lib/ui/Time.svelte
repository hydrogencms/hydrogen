<script lang="ts">
  import { formatDateTime, relativeTime } from '../format';

  /** A relative time ("2 h ago") with the exact time on hover. */
  let { at }: { at: string | Date | undefined | null } = $props();
  const iso = $derived(at instanceof Date ? at.toISOString() : (at ?? ''));
</script>

{#if iso}<time class="h-num" datetime={iso} title={formatDateTime(iso)}>{relativeTime(iso)}</time>{/if}
