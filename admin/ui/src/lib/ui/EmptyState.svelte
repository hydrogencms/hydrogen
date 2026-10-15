<script lang="ts">
  import type { Snippet } from 'svelte';
  import Icon from './Icon.svelte';
  let { title, description, icon = 'file', actions }: { title: string; description?: string; icon?: string; actions?: Snippet } = $props();
</script>

<div class="h-empty">
  <Icon name={icon} size={32} />
  <h2 class="h-empty__title">{title}</h2>
  {#if description}<p class="h-empty__text">{description}</p>{/if}
  {#if actions}<div class="h-empty__actions">{@render actions()}</div>{/if}
</div>
