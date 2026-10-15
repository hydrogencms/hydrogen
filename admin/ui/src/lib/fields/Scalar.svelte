<script lang="ts">
  import { untrack } from 'svelte';
  import DatePicker from '../ui/DatePicker.svelte';
  import type { FieldControl } from '../ui/Field.svelte';
  import Input from '../ui/Input.svelte';
  import Select from '../ui/Select.svelte';
  import Switch from '../ui/Switch.svelte';
  import Textarea from '../ui/Textarea.svelte';
  import type { CompiledField } from '../types.svelte';
  import { t } from '../i18n';
  import JsonBox from './JsonBox.svelte';
  import Reference from './Reference.svelte';
  import { useFields } from './context';
  import { autoSlug, fromLocalInput, slugify, slugSource, toLocalInput, type Doc } from './logic';

  let {
    field,
    value = $bindable(),
    doc,
    path,
    readonly = false,
    control,
    ariaLabel,
  }: {
    field: CompiledField;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    value: any;
    /** Sibling values (a slug follows its source field). */
    doc?: Doc;
    path: string;
    readonly?: boolean;
    /** From the surrounding Field; absent for list items, which are labelled with `ariaLabel`. */
    control?: FieldControl;
    ariaLabel?: string;
  } = $props();

  const ctx = useFields();
  const base = $derived(field.shape.base);
  const format = $derived(field.shape.format ?? '');
  const invalid = $derived(control ? control.invalid : !!ctx.error(path));
  const attrs = $derived(
    control
      ? { id: control.id, 'aria-describedby': control['aria-describedby'], 'aria-invalid': control['aria-invalid'] }
      : { 'aria-label': ariaLabel ?? field.label, 'aria-invalid': invalid ? (true as const) : undefined },
  );
  const placeholder = $derived(field.admin?.placeholder);
  const str = $derived(value == null ? '' : String(value));

  function set(v: unknown) {
    value = v;
    ctx.clearError(path);
  }

  // ---- slug: follows the source field while empty (or while the item is new and still generated)
  const source = $derived(format === 'slug' && doc ? slugSource(field, ctx.namePattern) : undefined);
  let last: string | undefined;
  $effect(() => {
    if (!source || !doc) return;
    const next = String(doc[source] ?? '');
    untrack(() => {
      if (last === undefined) {
        last = next;
        return;
      }
      if (next === last) return;
      const cur = typeof value === 'string' ? value : '';
      if (cur === '' || ctx.isNew) {
        const n = autoSlug(cur, last, next);
        if (n !== cur) value = n;
      }
      last = next;
    });
  });

  function num(e: Event) {
    const raw = (e.currentTarget as HTMLInputElement).value;
    set(raw === '' ? undefined : Number(raw));
  }
</script>

{#if base === 'ref'}
  <Reference {field} bind:value {path} {readonly} {invalid} {attrs} />
{:else if base === 'richtext' || base === 'json' || base === 'group' || base === 'list' || (base !== 'string' && base !== 'number' && base !== 'boolean')}
  <JsonBox {field} bind:value {path} {readonly} {invalid} {attrs} rich={base === 'richtext'} />
{:else if base === 'boolean'}
  <Switch {...attrs} checked={value === true} disabled={readonly} onchange={(e) => set((e.currentTarget as HTMLInputElement).checked)} />
{:else if base === 'number'}
  <Input {...attrs} type="number" step={field.shape.integer ? 1 : 'any'} value={str} {invalid} {placeholder} disabled={readonly} oninput={num} />
{:else if format === 'enum'}
  <Select
    {...attrs}
    value={str}
    {invalid}
    disabled={readonly}
    placeholder={placeholder ?? (field.required ? t('collections.choose') : t('collections.none'))}
    options={(field.shape.enum ?? []).map((v) => ({ value: v, label: v }))}
    onchange={(e) => set((e.currentTarget as HTMLSelectElement).value)}
  />
{:else if format === 'text'}
  <Textarea {...attrs} rows={5} value={str} {invalid} {placeholder} disabled={readonly} oninput={(e) => set((e.currentTarget as HTMLTextAreaElement).value)} />
{:else if format === 'date'}
  <DatePicker {...attrs} type="date" value={str} {invalid} disabled={readonly} oninput={(e) => set((e.currentTarget as HTMLInputElement).value)} />
{:else if format === 'datetime'}
  <DatePicker
    {...attrs}
    type="datetime-local"
    value={toLocalInput(value)}
    {invalid}
    disabled={readonly}
    oninput={(e) => set(fromLocalInput((e.currentTarget as HTMLInputElement).value))}
  />
{:else}
  <Input
    {...attrs}
    type="text"
    inputmode={format === 'email' ? 'email' : format === 'url' ? 'url' : undefined}
    autocomplete="off"
    spellcheck={format === 'slug' || format === 'email' || format === 'url' ? false : undefined}
    value={str}
    {invalid}
    {placeholder}
    disabled={readonly}
    oninput={(e) => set((e.currentTarget as HTMLInputElement).value)}
    onblur={format === 'slug' ? () => str && slugify(str) !== str && set(slugify(str)) : undefined}
  />
{/if}
