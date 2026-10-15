<script lang="ts">
  /** The product mark: a rounded tile with a geometric H. Follows the accent (black in light, white in dark). */
  let { size = 28 }: { size?: number } = $props();
</script>

<svg class="h-logo" width={size} height={size} viewBox="0 0 28 28" aria-hidden="true" focusable="false">
  <rect class="h-logo__tile" width="28" height="28" rx="8" />
  <path class="h-logo__mark" d="M8.5 7h3v14h-3zM16.5 7h3v14h-3zM11.5 12.5h5v3h-5z" />
</svg>
