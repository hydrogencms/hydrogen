<script lang="ts">
  import Editor from './Editor.svelte';

  let { params, id, embedded = false }: { params?: Record<string, string>; id?: string; embedded?: boolean } = $props();
</script>

<Editor mode="item" id={id ?? params?.id ?? ''} {embedded} />
