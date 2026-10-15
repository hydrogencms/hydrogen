<script lang="ts">
  import type { CompiledField } from '../types.svelte';
  import Fields from './Fields.svelte';
  import { useFields } from './context';

  let {
    field,
    value = $bindable(),
    path,
    readonly = false,
    bare = false,
  }: {
    field: CompiledField;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    value: any;
    path: string;
    readonly?: boolean;
    /** No legend (a list item draws its own heading). */
    bare?: boolean;
  } = $props();

  const ctx = useFields();
  const error = $derived(ctx.error(path));
</script>

<fieldset class="h-fieldset" disabled={readonly}>
  {#if !bare}
    <legend class="h-fieldset__legend">
      {field.label}{#if field.required}<span class="h-field__req" aria-hidden="true">*</span>{/if}
    </legend>
  {/if}
  {#if field.description && !bare}<p class="h-field__desc">{field.description}</p>{/if}
  {#if error}<p class="h-field__error" role="alert">{error}</p>{/if}
  {#if value && typeof value === 'object'}
    <div class="h-fieldset__body">
      <Fields fields={field.fields ?? []} bind:doc={value} {path} />
    </div>
  {/if}
</fieldset>
