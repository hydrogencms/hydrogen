<script lang="ts">
  import { icons } from './icons';

  let { name, size = 18, label }: { name: string; size?: number; label?: string } = $props();
  const id = $derived(name in icons ? name : 'file');
</script>

<svg
  class="h-icon"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  role={label ? 'img' : undefined}
  aria-label={label}
  aria-hidden={label ? undefined : 'true'}
  focusable="false"
>
  <use href={`#h-i-${id}`} />
</svg>
