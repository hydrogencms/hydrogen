<script lang="ts">
  // Loads the mount code only when a slot actually renders an element.
  let {
    tag,
    props = {},
    onEvents = {},
  }: { tag: string; props?: Record<string, unknown>; onEvents?: Record<string, (e: CustomEvent) => void> } = $props();
</script>

{#await import('./ExtensionHost.svelte') then m}
  <m.default {tag} {props} {onEvents} />
{/await}
