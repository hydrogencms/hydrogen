<script lang="ts">
  let {
    lines = 1,
    height = '1em',
    width = '100%',
    shape = 'line',
  }: {
    lines?: number;
    height?: string;
    width?: string;
    /** `line`: text lines (the last is shorter); `circle`: avatar; `block`: a rounded card/image block. */
    shape?: 'line' | 'circle' | 'block';
  } = $props();
</script>

<div class="h-skeleton" aria-hidden="true">
  {#each Array(lines) as _, i (i)}
    <span class="h-skeleton__line h-skeleton__line--{shape}" style:height style:width={shape === 'line' && lines > 1 && i === lines - 1 ? '60%' : width}></span>
  {/each}
</div>
