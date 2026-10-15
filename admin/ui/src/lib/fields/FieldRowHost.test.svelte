<script lang="ts">
  import FieldRow from './FieldRow.svelte';
  import { provideFields } from './context';
  import type { CompiledField } from '../types.svelte';
  import type { Form } from '../form.svelte';

  let { form, field }: { form: Form<{ fields: Record<string, unknown> }>; field: CompiledField } = $props();
  provideFields({ error: (p) => form.error(p), clearError: () => {}, readonly: false, locale: 'en', isNew: false });
</script>

<FieldRow {field} bind:value={form.values.fields[field.name]} doc={form.values.fields} path={field.name} />
