<script lang="ts">
  import type { Snippet } from 'svelte';
  import { focusables, portal, trapTab } from './focus';
  import IconButton from './IconButton.svelte';
  import { t } from '../i18n';

  let {
    open = $bindable(false),
    title,
    onclose,
    size = 'md',
    dismissible = true,
    children,
    footer,
  }: {
    open?: boolean;
    title: string;
    onclose?: () => void;
    size?: 'sm' | 'md' | 'lg';
    /** false: Esc and backdrop clicks do not close (use for destructive confirmations in flight). */
    dismissible?: boolean;
    children: Snippet;
    footer?: Snippet;
  } = $props();

  const uid = $props.id();
  let box = $state<HTMLElement>();

  function close() {
    open = false;
    onclose?.();
  }

  // Focus moves in on open (first focusable that is not the close button, else the dialog) and returns to
  // the element that had it when the dialog closes.
  $effect(() => {
    if (!open || !box) return;
    const before = document.activeElement as HTMLElement | null;
    const f = focusables(box);
    (f.find((e) => !e.classList.contains('h-dialog__x')) ?? f[0] ?? box).focus();
    document.documentElement.classList.add('h-noscroll');
    return () => {
      document.documentElement.classList.remove('h-noscroll');
      before?.focus?.();
    };
  });

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && dismissible) {
      e.stopPropagation();
      close();
    } else if (box) trapTab(e, box);
  }
</script>

{#if open}
  <div class="h-dialog" use:portal>
    <div class="h-dialog__backdrop" role="presentation" onclick={() => dismissible && close()}></div>
    <div
      bind:this={box}
      class="h-dialog__box h-dialog__box--{size}"
      role="dialog"
      aria-modal="true"
      aria-labelledby="{uid}-t"
      tabindex="-1"
      onkeydown={keydown}
    >
      <header class="h-dialog__head">
        <h2 class="h-dialog__title" id="{uid}-t">{title}</h2>
        {#if dismissible}<IconButton class="h-dialog__x" icon="x" label={t('common.close')} size="sm" onclick={close} />{/if}
      </header>
      <div class="h-dialog__body">{@render children()}</div>
      {#if footer}<footer class="h-dialog__foot">{@render footer()}</footer>{/if}
    </div>
  </div>
{/if}
