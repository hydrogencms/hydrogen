<script lang="ts">
  import FieldRow from './FieldRow.svelte';
  import { provideFields, type OverlayCtx } from './context';
  import { overlayState, overrideValue } from './logic';
  import type { CompiledField } from '../types.svelte';

  let { fields, field }: { fields: Record<string, unknown>; field: CompiledField } = $props();
  // svelte-ignore state_referenced_locally
  const form = $state({ fields });
  const overlay: OverlayCtx = {
    parent: 'de',
    state: (n) => overlayState(form.fields, n),
    source: () => 'de',
    value: () => 'Hallo',
    resolved: () => form.fields,
    override: (n) => (form.fields[n] = overrideValue(field, 'Hallo')),
    reset: (n) => delete form.fields[n],
  };
  provideFields({ error: () => undefined, clearError: () => {}, readonly: false, locale: 'de-CH', isNew: false, overlay });
</script>

<FieldRow {field} bind:value={form.fields[field.name]} doc={form.fields} path={field.name} />
<output>{JSON.stringify(form.fields)}</output>
