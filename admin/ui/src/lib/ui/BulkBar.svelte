<script lang="ts">
  import type { Snippet } from 'svelte';
  import IconButton from './IconButton.svelte';
  import { t } from '../i18n';

  /** A floating pill at the bottom centre while rows are selected: "3 selected · actions · ×". */
  let { count, onclear, children }: { count: number; onclear: () => void; children?: Snippet } = $props();
</script>

{#if count > 0}
  <div class="h-bulkbar" role="region" aria-label={t('bulk.label')}>
    <strong class="h-num" aria-live="polite">{t('bulk.selected', { n: count })}</strong>
    {@render children?.()}
    <IconButton icon="x" label={t('bulk.clear')} size="sm" onclick={onclear} />
  </div>
{/if}
