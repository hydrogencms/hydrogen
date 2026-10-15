<script lang="ts">
  import Textarea from '../ui/Textarea.svelte';
  import type { CompiledField } from '../types.svelte';
  import { t } from '../i18n';
  import { useFields } from './context';

  let {
    field,
    value = $bindable(),
    path,
    readonly = false,
    invalid = false,
    attrs = {},
  }: {
    field: CompiledField;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    value: any;
    path: string;
    readonly?: boolean;
    invalid?: boolean;
    attrs?: Record<string, unknown>;
  } = $props();

  const ctx = useFields();
  const pretty = (v: unknown) => (v === undefined ? '' : JSON.stringify(v, null, 2));
  let text = $state(pretty(value));
  let editing = $state(false);
  let problem = $state('');

  // Follow outside changes (locale switch, copy from the main locale) unless the user is typing.
  $effect(() => {
    const p = pretty(value);
    if (!editing) {
      text = p;
      problem = '';
    }
  });

  function commit() {
    editing = false;
    const raw = text.trim();
    if (raw === '') {
      problem = '';
      if (value !== undefined) value = undefined;
      ctx.clearError(path);
      return;
    }
    try {
      const v = JSON.parse(raw);
      problem = '';
      value = v;
      ctx.clearError(path);
    } catch (e) {
      problem = t('collections.jsonInvalid', { message: (e as Error).message });
    }
  }
</script>

<div class="h-jsonbox">
  <Textarea
    {...attrs}
    rows={6}
    class="h-jsonbox__area"
    value={text}
    invalid={invalid || !!problem}
    disabled={readonly}
    spellcheck={false}
    onfocus={() => (editing = true)}
    oninput={(e) => (text = (e.currentTarget as HTMLTextAreaElement).value)}
    onblur={commit}
  />
  {#if problem}<p class="h-field__error" role="alert">{problem}</p>{/if}
</div>
