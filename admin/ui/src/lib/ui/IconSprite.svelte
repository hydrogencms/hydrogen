<script lang="ts">
  import { icons } from './icons';
</script>

<svg class="h-sprite" width="0" height="0" aria-hidden="true" focusable="false">
  <defs>
    {#each Object.entries(icons) as [name, d] (name)}
      <symbol id={`h-i-${name}`} viewBox="0 0 24 24"><path {d} /></symbol>
    {/each}
  </defs>
</svg>
