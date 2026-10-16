<script lang="ts">
  import { router } from '../../lib/router.svelte';
  import ReleaseDetail from '../release/ReleaseDetail.svelte';
  import Releases from '../release/Releases.svelte';
  import Trash from '../trash/Trash.svelte';
  import TrashedItem from '../trash/TrashedItem.svelte';

  let { params }: { params?: Record<string, string> } = $props();
  const id = $derived(params?.rest?.split('/')[0] ?? '');
  const trash = $derived(router.route?.path === '/trash/*');
</script>

{#if trash}
  {#if id}<TrashedItem params={{ id }} />{:else}<Trash />{/if}
{:else if id}
  <ReleaseDetail params={{ id }} />
{:else}
  <Releases />
{/if}
