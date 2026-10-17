<script lang="ts">
  import Fields from '../fields/Fields.svelte';
  import { provideFields } from '../fields/context';
  import type { Doc } from '../fields/logic';
  import type { CompiledField } from '../types.svelte';

  // The project's own asset fields (alt, caption, …) rendered with the item editor's widgets.
  let {
    fields,
    doc = $bindable(),
    locale,
    type,
    errors = {},
    readonly = false,
    onedit,
  }: {
    fields: CompiledField[];
    doc: Doc;
    locale: string;
    type: string;
    errors?: Record<string, string>;
    readonly?: boolean;
    /** A field changed (its path): the caller clears that field's error. */
    onedit?: (path: string) => void;
  } = $props();

  provideFields({
    error: (p) => errors[p],
    clearError: (p) => onedit?.(p),
    get readonly() {
      return readonly;
    },
    get locale() {
      return locale;
    },
    get type() {
      return type;
    },
    isNew: false,
  });
</script>

<div class="h-fieldgrid">
  <Fields {fields} bind:doc />
</div>
