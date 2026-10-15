<script lang="ts">
  import type { Component } from 'svelte';
  import { onMount } from 'svelte';
  import { t } from '../i18n';
  import '../messages/richtext';
  import Skeleton from '../ui/Skeleton.svelte';
  import type { RichTextProps } from './types';

  // The rich text editor. ProseMirror and the editor load as their own chunk when the first one renders;
  // until then a skeleton holds the place. Props: see RichTextProps (types.ts).
  let props: RichTextProps = $props();

  let Impl = $state<Component<RichTextProps> | null>(null);
  let failed = $state(false);
  onMount(() => {
    import('./Editor.svelte').then(
      (m) => (Impl = m.default as unknown as Component<RichTextProps>),
      () => (failed = true),
    );
  });
</script>

{#if Impl}
  <Impl {...props} />
{:else if failed}
  <p class="h-field__error" role="alert">{t('richtext.loadFailed')}</p>
{:else}
  <div class="h-rte-skel" role="status" aria-busy="true" aria-label={t('richtext.loading')}>
    <Skeleton lines={4} />
  </div>
{/if}

<style>
  .h-rte-skel {
    min-height: calc(var(--h-rte-rows, 8) * 1.6em + 3rem);
    padding: var(--h-space-4);
    border: 1px solid var(--h-border-strong);
    border-radius: var(--h-radius);
    background: var(--h-surface);
  }
</style>
