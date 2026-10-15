<script lang="ts">
  import type { CompiledField } from '../types.svelte';
  import './fields.css';
  import FieldRow from './FieldRow.svelte';
  import { joinPath, type Doc } from './logic';

  let { fields, doc = $bindable(), path = '' }: { fields: CompiledField[]; doc: Doc; path?: string } = $props();
</script>

{#each fields as f (f.name)}
  <FieldRow field={f} bind:value={doc[f.name]} {doc} path={joinPath(path, f.name)} />
{/each}
