<script lang="ts">
  import type { Snippet } from 'svelte';
  import { t } from '../i18n';
  import Icon from './Icon.svelte';

  export interface FieldControl {
    id: string;
    'aria-describedby': string | undefined;
    'aria-invalid': true | undefined;
    invalid: boolean;
  }
  let {
    label,
    description,
    error,
    required = false,
    id: given,
    children,
  }: {
    label: string;
    /** Muted help text, shown under the label (above the control). */
    description?: string;
    error?: string;
    required?: boolean;
    id?: string;
    /** Receives the props to spread on the control: `{#snippet children(c)}<Input {...c} bind:value />{/snippet}` */
    children: Snippet<[FieldControl]>;
  } = $props();

  const uid = $props.id();
  const id = $derived(given ?? `f-${uid}`);
  const describedby = $derived([description ? `${id}-d` : '', error ? `${id}-e` : ''].filter(Boolean).join(' ') || undefined);
</script>

<div class="h-field" class:h-field--invalid={!!error}>
  <label class="h-field__label" for={id}>
    {label}{#if required}<span class="h-field__req" title={t('common.required')} aria-hidden="true">*</span>{/if}
  </label>
  {#if description}<p class="h-field__desc" id="{id}-d">{description}</p>{/if}
  {@render children({ id, 'aria-describedby': describedby, 'aria-invalid': error ? true : undefined, invalid: !!error })}
  {#if error}<p class="h-field__error" id="{id}-e" role="alert"><Icon name="alert-circle" size={14} />{error}</p>{/if}
</div>
