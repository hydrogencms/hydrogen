<script lang="ts">
  import { t } from '../i18n';
  import Button from '../ui/Button.svelte';
  import Checkbox from '../ui/Checkbox.svelte';
  import Input from '../ui/Input.svelte';
  import Select from '../ui/Select.svelte';
  import type { RichAttrDef } from './config';
  import { attrOk } from './schema';

  let {
    attrs,
    values,
    labels = {},
    submit: submitLabel,
    removeLabel,
    check,
    onapply,
    onremove,
  }: {
    attrs: RichAttrDef[];
    /** The current values (edit) or the defaults (insert). */
    values: Record<string, unknown>;
    /** Field labels by attribute name (the name itself when missing). */
    labels?: Record<string, string>;
    submit: string;
    removeLabel?: string;
    /** Extra validation of the typed values: a message per attribute name. */
    check?: (v: Record<string, unknown>) => Record<string, string>;
    /** Every attribute, null for the empty ones. */
    onapply: (v: Record<string, unknown>) => void;
    onremove?: () => void;
  } = $props();

  const uid = $props.id();
  // The form is created each time its dialog opens: the initial values are all it needs.
  const initial = (a: RichAttrDef): string | boolean => {
    const v = values[a.name] ?? a.default;
    return a.type === 'bool' ? v === true : v === null || v === undefined ? '' : String(v);
  };
  // svelte-ignore state_referenced_locally
  let form = $state<Record<string, string | boolean>>(Object.fromEntries(attrs.map((a) => [a.name, initial(a)])));
  let problems = $state<Record<string, string>>({});

  function parse(a: RichAttrDef): { value: unknown } | { error: string } {
    const raw = form[a.name];
    if (a.type === 'bool') return { value: raw === true ? true : a.default === true ? false : null };
    const s = String(raw).trim();
    if (s === '') return a.required ? { error: t('richtext.attrRequired') } : { value: null };
    if (a.type === 'int') {
      const n = Number(s);
      if (!attrOk(a, n)) return { error: t('richtext.attrInt', { min: a.min ?? -1e9, max: a.max ?? 1e9 }) };
      return { value: n };
    }
    const v = a.type === 'string' ? String(raw) : s;
    if (!attrOk(a, v)) return { error: a.type === 'url' ? t('richtext.attrUrl') : a.type === 'enum' ? t('richtext.attrRequired') : t('richtext.attrLong', { max: a.maxLength ?? 500 }) };
    return { value: v };
  }

  function submit(e: Event) {
    e.preventDefault();
    const out: Record<string, unknown> = {};
    const bad: Record<string, string> = {};
    for (const a of attrs) {
      const r = parse(a);
      if ('error' in r) bad[a.name] = r.error;
      else out[a.name] = r.value;
    }
    if (Object.keys(bad).length === 0) Object.assign(bad, check?.(out) ?? {});
    problems = bad;
    if (Object.keys(bad).length === 0) onapply(out);
  }
  const name = (a: RichAttrDef) => labels[a.name] ?? a.name;
</script>

<form class="h-rte__attrs" onsubmit={submit} novalidate>
  {#each attrs as a (a.name)}
    {@const err = problems[a.name]}
    {#if a.type === 'bool'}
      <Checkbox label={name(a)} bind:checked={form[a.name] as boolean} />
    {:else}
      <label class="h-field__label" for="{uid}-{a.name}">{name(a)}{a.required ? ' *' : ''}</label>
      {#if a.type === 'enum'}
        <Select
          id="{uid}-{a.name}"
          bind:value={form[a.name] as string}
          options={(a.values ?? []).map((v) => ({ value: v, label: v }))}
          placeholder={a.required ? t('richtext.attrChoose') : t('richtext.attrNone')}
          invalid={!!err}
          aria-describedby={err ? `${uid}-${a.name}-e` : undefined}
        />
      {:else if a.type === 'int'}
        <Input id="{uid}-{a.name}" type="number" min={a.min} max={a.max} step="1" bind:value={form[a.name] as string} invalid={!!err} aria-describedby={err ? `${uid}-${a.name}-e` : undefined} />
      {:else}
        <Input id="{uid}-{a.name}" maxlength={a.type === 'string' ? (a.maxLength ?? 500) : undefined} bind:value={form[a.name] as string} invalid={!!err} aria-describedby={err ? `${uid}-${a.name}-e` : undefined} />
      {/if}
      {#if err}<p class="h-field__error" id="{uid}-{a.name}-e" role="alert">{err}</p>{/if}
    {/if}
  {/each}
  <div class="h-rte__linkactions">
    {#if onremove}<Button size="sm" variant="ghost" onclick={onremove}>{removeLabel}</Button>{/if}
    <span class="h-spacer"></span>
    <Button size="sm" variant="primary" type="submit">{submitLabel}</Button>
  </div>
</form>
