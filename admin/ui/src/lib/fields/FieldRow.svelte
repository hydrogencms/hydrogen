<script lang="ts">
  import Field from '../ui/Field.svelte';
  import type { CompiledField } from '../types.svelte';
  import GroupWidget from './GroupWidget.svelte';
  import ListWidget from './ListWidget.svelte';
  import Scalar from './Scalar.svelte';
  import { useFields } from './context';
  import { evalWhen, type Doc } from './logic';

  let {
    field,
    value = $bindable(),
    doc,
    path,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  }: { field: CompiledField; value: any; doc: Doc; path: string } = $props();

  const ctx = useFields();
  const shown = $derived(!field.admin?.hidden && evalWhen(field.when, doc));
  const base = $derived(field.shape.base);
  const nested = $derived(base === 'list' || (base === 'group' && !!field.fields));
  const readonly = $derived(ctx.readonly || !!field.admin?.readonly);
</script>

{#if shown}
  <div class="h-fieldrow" data-field={path}>
    {#if base === 'list'}
      <ListWidget {field} bind:value {path} {readonly} />
    {:else if nested}
      <GroupWidget {field} bind:value {path} {readonly} />
    {:else}
      <Field label={field.label} description={field.description} error={ctx.error(path)} required={field.required}>
        {#snippet children(c)}
          <Scalar {field} bind:value {doc} {path} {readonly} control={c} />
        {/snippet}
      </Field>
    {/if}
  </div>
{/if}
