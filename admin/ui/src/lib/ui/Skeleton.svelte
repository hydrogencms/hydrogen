<script lang="ts">
  let { lines = 1, height = '1em', width = '100%' }: { lines?: number; height?: string; width?: string } = $props();
</script>

<div class="h-skeleton" aria-hidden="true">
  {#each Array(lines) as _, i (i)}
    <span class="h-skeleton__line" style:height style:width={lines > 1 && i === lines - 1 ? '60%' : width}></span>
  {/each}
</div>
