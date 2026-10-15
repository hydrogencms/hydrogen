<script lang="ts">
  import Field from '../ui/Field.svelte';
  import Skeleton from '../ui/Skeleton.svelte';
  import type { CompiledField } from '../types.svelte';
  import GroupWidget from './GroupWidget.svelte';
  import ListWidget from './ListWidget.svelte';
  import Scalar from './Scalar.svelte';
  import { useFields } from './context';
  import { ext } from '../extensions.svelte';
  import Extension from '../ext/Extension.svelte';
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
  const widget = $derived(ext.widget(field.admin?.widget));
  const isBlocks = $derived(base === 'blocks');
  const nested = $derived(base === 'list' || (base === 'group' && !!field.fields));
  // Short fields flow two or three to a row in the editor's field grid; everything else spans the card.
  const short = $derived(
    !widget && !isBlocks && (base === 'number' || base === 'boolean' || base === 'ref' || (base === 'string' && ['enum', 'date', 'datetime'].includes(field.shape.format ?? ''))),
  );
  const readonly = $derived(ctx.readonly || !!field.admin?.readonly);
</script>

{#if shown}
  <div class="h-fieldrow" class:h-fieldrow--wide={!short} data-field={path}>
    {#if widget}
      <Field label={field.label} description={field.description} required={field.required}>
        {#snippet children()}
          <Extension
            tag={widget}
            props={{ value: $state.snapshot(value), field, locale: ctx.locale, readonly, error: ctx.error(path) }}
            onEvents={{
              change: (e) => {
                value = e.detail?.value;
                ctx.clearError(path);
              },
            }}
          />
        {/snippet}
      </Field>
    {:else if isBlocks}
      {#await import('../blocks/BlocksField.svelte')}
        <Skeleton shape="block" height="6rem" />
      {:then m}
        {@const Blocks = m.default}
        <Blocks {field} bind:value {path} {readonly} />
      {/await}
    {:else if base === 'list'}
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
