<script lang="ts">
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Checkbox from '../../lib/ui/Checkbox.svelte';
  import { t } from '../../lib/i18n';
  import { formatValues, parseValues, setLimit, toggleValue, type LimitDef, type Limits } from './limits';

  let {
    names,
    defs,
    value = $bindable({}),
    errors = {},
    prefix = 'lim',
  }: {
    /** Limitation names to offer (the ones that apply). */
    names: string[];
    defs: LimitDef[];
    value?: Limits;
    /** Messages by limitation name. */
    errors?: Record<string, string | undefined>;
    prefix?: string;
  } = $props();

  const shown = $derived(names.map((n) => defs.find((d) => d.name === n)).filter((d): d is LimitDef => !!d));
  // Free-text boxes keep their own text so "a, " can be typed; the list is re-derived on every input.
  let texts = $state<Record<string, string>>({});
  const text = (n: string) => {
    const raw = texts[n];
    return raw !== undefined && formatValues(parseValues(raw)) === formatValues(value[n]) ? raw : formatValues(value[n]);
  };

  function labelOf(def: LimitDef, v: string) {
    return def.choices?.find((c) => c.value === v)?.label ?? v;
  }
  function options(def: LimitDef) {
    const known = def.choices ?? [];
    const extra = (value[def.name] ?? []).filter((v) => !known.some((c) => c.value === v));
    return [...known, ...extra.map((v) => ({ value: v, label: labelOf(def, v) }))];
  }
</script>

{#if shown.length === 0}
  <p class="h-muted">{t('settings.lim.none')}</p>
{:else}
  <div class="h-stack">
    {#each shown as def (def.name)}
      {#if def.choices}
        <fieldset class="h-lim">
          <legend>{def.name}</legend>
          <div class="h-lim__choices">
            {#each options(def) as o (o.value)}
              <Checkbox
                label={o.label}
                checked={(value[def.name] ?? []).includes(o.value)}
                onchange={(e) => (value = setLimit(value, def.name, toggleValue(value[def.name], o.value, e.currentTarget.checked)))} />
            {/each}
          </div>
          {#if errors[def.name]}<p class="h-field__error" role="alert">{errors[def.name]}</p>{/if}
        </fieldset>
      {:else}
        <Field label={def.name} description={t('settings.lim.freeHint')} error={errors[def.name]} id={`${prefix}-${def.name}`}>
          {#snippet children(c)}
            <Input
              {...c}
              value={text(def.name)}
              oninput={(e) => {
                texts[def.name] = e.currentTarget.value;
                value = setLimit(value, def.name, parseValues(e.currentTarget.value));
              }} />
          {/snippet}
        </Field>
      {/if}
    {/each}
    {#if shown.some((d) => d.choices)}<p class="h-muted">{t('settings.lim.choicesHint')}</p>{/if}
  </div>
{/if}

<style>
  .h-lim {
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius);
    padding: var(--h-space-3);
  }
  .h-lim legend {
    padding: 0 var(--h-space-2);
    font-weight: 600;
  }
  .h-lim__choices {
    display: flex;
    flex-wrap: wrap;
    gap: var(--h-space-2) var(--h-space-4);
  }
</style>
