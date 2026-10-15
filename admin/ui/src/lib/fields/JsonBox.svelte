<script lang="ts">
  import Button from '../ui/Button.svelte';
  import Textarea from '../ui/Textarea.svelte';
  import type { CompiledField } from '../types.svelte';
  import { t } from '../i18n';
  import { useFields } from './context';
  import { richPreview } from './logic';

  let {
    field,
    value = $bindable(),
    path,
    readonly = false,
    invalid = false,
    attrs = {},
    rich = false,
  }: {
    field: CompiledField;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    value: any;
    path: string;
    readonly?: boolean;
    invalid?: boolean;
    attrs?: Record<string, unknown>;
    /** Rich text: shows a rendered preview above the JSON. */
    rich?: boolean;
  } = $props();

  const ctx = useFields();
  const pretty = (v: unknown) => (v === undefined ? '' : JSON.stringify(v, null, 2));
  let text = $state(pretty(value));
  let editing = $state(false);
  let problem = $state('');
  let open = $state(false);

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
      if (rich && (typeof v !== 'object' || v === null || v.type !== 'doc')) {
        problem = t('collections.richNotDoc');
        return;
      }
      problem = '';
      value = v;
      ctx.clearError(path);
    } catch (e) {
      problem = t('collections.jsonInvalid', { message: (e as Error).message });
    }
  }

  const lines = $derived(rich ? richPreview(value) : []);
  const shown = $derived(!rich || open);
</script>

<div class="h-jsonbox">
  {#if rich}
    <div class="h-jsonbox__preview" role="group" aria-label={t('collections.preview', { field: field.label })}>
      {#each lines as l, i (i)}
        {#if l.kind === 'heading'}
          <p class="h-jsonbox__h">{l.text}</p>
        {:else if l.kind === 'code'}
          <pre class="h-jsonbox__code">{l.text}</pre>
        {:else if l.kind === 'rule'}
          <hr />
        {:else}
          <p class:h-jsonbox__quote={l.kind === 'quote'}>{l.text}</p>
        {/if}
      {:else}
        <p class="h-muted">{t('collections.richEmpty')}</p>
      {/each}
    </div>
    <Button size="sm" aria-expanded={open} onclick={() => (open = !open)}>{open ? t('collections.hideJson') : t('collections.editJson')}</Button>
    <p class="h-field__desc">{t('collections.richNote')}</p>
  {/if}
  {#if shown}
    <Textarea
      {...attrs}
      rows={rich ? 10 : 6}
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
  {/if}
</div>
