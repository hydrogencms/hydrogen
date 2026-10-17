<script lang="ts">
  import Field from '../ui/Field.svelte';
  import Skeleton from '../ui/Skeleton.svelte';
  import type { CompiledField } from '../types.svelte';
  import GroupWidget from './GroupWidget.svelte';
  import ListWidget from './ListWidget.svelte';
  import Scalar from './Scalar.svelte';
  import { useFields } from './context';
  import { ext } from '../extensions.svelte';
  import Extension from '../ext/Extension.svelte';
  import { evalWhen, type Doc } from './logic';
  import Badge from '../ui/Badge.svelte';
  import Button from '../ui/Button.svelte';
  import '../messages/overlays';
  import { t } from '../i18n';

  let {
    field,
    value = $bindable(),
    doc,
    path,
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  }: { field: CompiledField; value: any; doc: Doc; path: string } = $props();

  const ctx = useFields();
  // Locale inheritance: a top-level translatable field is either inherited (the parent's value, read-only) or overridden.
  const ov = $derived(ctx.overlay && field.translatable && path === field.name ? ctx.overlay : undefined);
  const inherited = $derived(!!ov && ov.state(field.name) === 'inherited');
  // An inherited value is shown through a copy, so a widget normalising it can never turn it into an override.
  let shadow = $state<any>(); // eslint-disable-line @typescript-eslint/no-explicit-any
  $effect.pre(() => {
    if (ov && inherited) shadow = structuredClone(ov.value(field.name));
  });
  const cur = () => (inherited ? shadow : value);
  const put = (v: any) => (inherited ? (shadow = v) : (value = v)); // eslint-disable-line @typescript-eslint/no-explicit-any
  const shown = $derived(!field.admin?.hidden && evalWhen(field.when, ctx.overlay && path === field.name ? ctx.overlay.resolved() : doc));
  const base = $derived(field.shape.base);
  const widget = $derived(ext.widget(field.admin?.widget));
  const isBlocks = $derived(base === 'blocks');
  const nested = $derived(base === 'list' || (base === 'group' && !!field.fields));
  // Short fields flow two or three to a row in the editor's field grid; everything else spans the card.
  const short = $derived(
    !widget && !isBlocks && (base === 'number' || base === 'boolean' || base === 'ref' || (base === 'string' && ['enum', 'date', 'datetime'].includes(field.shape.format ?? ''))),
  );
  // A part another editor is still seeding: the fields wait, read-only, until its first state arrives.
  const loading = $derived(!!ctx.loading?.(/^[^.[]+/.exec(path)?.[0] ?? path));
  const readonly = $derived(ctx.readonly || !!field.admin?.readonly || loading || inherited);
</script>

{#if shown}
  <div class="h-fieldrow" class:h-fieldrow--wide={!short} class:h-fieldrow--loading={loading} data-field={path} aria-busy={loading || undefined}>
    {#if loading && path === (/^[^.[]+/.exec(path)?.[0] ?? path)}<span class="h-fieldrow__loading h-muted" role="status">{t('collab.loading')}</span>{/if}
    {#if ov}
      <div class="h-fieldrow__overlay" data-overlay={inherited ? 'inherited' : 'overridden'}>
        {#if inherited}
          <Badge>{t('overlay.inheritedFrom', { locale: ov.source(field.name) })}</Badge>
        {:else}
          <Badge tone="info">{t('overlay.overridden')}</Badge>
        {/if}
        {#if !ctx.readonly && !loading}
          {#if inherited}
            <Button size="sm" variant="ghost" onclick={() => ov.override(field.name)}>{t('overlay.override')}</Button>
          {:else}
            <Button size="sm" variant="ghost" onclick={() => ov.reset(field.name)}>{t('overlay.reset', { locale: ov.parent })}</Button>
          {/if}
        {/if}
      </div>
    {/if}
    {#if widget}
      <Field label={field.label} description={field.description} required={field.required}>
        {#snippet children()}
          <Extension
            tag={widget}
            props={{ value: $state.snapshot(cur()), field, locale: ctx.locale, readonly, error: ctx.error(path) }}
            onEvents={{
              change: (e) => {
                put(e.detail?.value);
                ctx.clearError(path);
              },
            }}
          />
        {/snippet}
      </Field>
    {:else if isBlocks}
      {#await import('../blocks/BlocksField.svelte')}
        <Skeleton shape="block" height="6rem" />
      {:then m}
        {@const Blocks = m.default}
        <Blocks {field} bind:value={cur, put} {path} {readonly} />
      {/await}
    {:else if base === 'list'}
      <ListWidget {field} bind:value={cur, put} {path} {readonly} />
    {:else if nested}
      <GroupWidget {field} bind:value={cur, put} {path} {readonly} />
    {:else}
      <Field label={field.label} description={field.description} error={ctx.error(path)} required={field.required}>
        {#snippet children(c)}
          <Scalar {field} bind:value={cur, put} {doc} {path} {readonly} control={c} />
        {/snippet}
      </Field>
    {/if}
  </div>
{/if}
