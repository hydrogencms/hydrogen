<script lang="ts">
  import Palette from '../blocks/Palette.svelte';
  import type { BlocksModel } from '../blocks/model.svelte';

  let { model, label, onopen }: { model: BlocksModel; label: string; onopen?: () => void } = $props();
  let open = $state(false);
  $effect(() => {
    if (open) onopen?.();
  });
</script>

<Palette {model} container={{}} index={0} variant="canvas-text" {label} bind:open />
