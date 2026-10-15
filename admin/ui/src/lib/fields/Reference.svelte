<script lang="ts">
  import { untrack } from 'svelte';
  import { api } from '../api/client';
  import Badge from '../ui/Badge.svelte';
  import Combobox, { type ComboOption } from '../ui/Combobox.svelte';
  import { types } from '../types.svelte';
  import type { CompiledField } from '../types.svelte';
  import { t } from '../i18n';
  import { href, link } from '../router.svelte';
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
  const targets = $derived((field.shape.to as string[] | undefined) ?? []);
  let selected = $state<ComboOption | null>(null);
  let info = $state<{ id: string; name: string; type: string; status: string } | null>(null);

  const hint = (type: string, status?: string) =>
    [types.get(type)?.label ?? type, status ? t(`collections.status.${status}`) : ''].filter(Boolean).join(' · ');

  // The box shows the selected item's name: resolved from its id.
  $effect(() => {
    const id = typeof value === 'string' ? value : '';
    untrack(() => {
      if (!id) {
        selected = null;
        info = null;
        return;
      }
      if (selected?.value === id) return;
      selected = { value: id, label: id };
    });
    if (!id) return;
    const ctl = new AbortController();
    api
      .get('/items/{id}', { params: { id }, query: { draft: true, locale: ctx.locale || undefined } as never, signal: ctl.signal })
      .then((v) => {
        selected = { value: id, label: v.name || id, hint: hint(v.type, v.status) };
        info = { id, name: v.name || id, type: v.type, status: v.status };
      })
      .catch(() => {});
    return () => ctl.abort();
  });

  async function search(q: string, signal: AbortSignal): Promise<ComboOption[]> {
    const per = await Promise.all(
      targets.map((type) =>
        api
          .get('/collections/{type}/items', { params: { type }, query: { q, limit: 10, locale: ctx.locale || undefined } as never, signal })
          .then((r) => r.items.map((i) => ({ value: i.id, label: i.name || i.id, hint: hint(i.type, i.status) }))),
      ),
    );
    return per.flat();
  }

  function pick(o: ComboOption | null) {
    value = o?.value;
    ctx.clearError(path);
  }
</script>

<div class="h-ref">
<Combobox
  {...attrs}
  value={selected}
  {search}
  {invalid}
  disabled={readonly}
  placeholder={field.admin?.placeholder ?? t('collections.refSearch', { types: targets.map((x) => types.get(x)?.label ?? x).join(', ') })}
  onselect={pick}
/>
{#if info && info.id === (typeof value === 'string' ? value : '')}
  <a class="h-ref__chip" use:link href={href(`/items/${info.id}`)}>
    <span class="h-ref__name">{info.name}</span>
    <span class="h-ref__type">{types.get(info.type)?.label ?? info.type}</span>
    <Badge tone={info.status === 'published' ? 'success' : info.status === 'changed' ? 'warning' : 'neutral'}>{t(`collections.status.${info.status}`)}</Badge>
  </a>
{/if}
</div>
