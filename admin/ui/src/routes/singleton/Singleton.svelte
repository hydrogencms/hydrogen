<script lang="ts">
  import Editor from '../item/Editor.svelte';

  let { params }: { params: Record<string, string> } = $props();
</script>

<Editor mode="singleton" typeName={params.type} />
