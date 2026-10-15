<script lang="ts">
  import '../../lib/messages/collections';
  import Editor from '../item/Editor.svelte';

  let { params }: { params: Record<string, string> } = $props();
</script>

<Editor mode="new" typeName={params.type} />
