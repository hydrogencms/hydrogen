<script lang="ts">
  import { api } from '../api/client';
  import { session } from '../auth/session.svelte';
  import { t } from '../i18n';
  import { types } from '../types.svelte';
  import Button from '../ui/Button.svelte';
  import Combobox, { type ComboOption } from '../ui/Combobox.svelte';
  import Input from '../ui/Input.svelte';
  import { normalizeHref, validHref } from './schema';

  let {
    href = '',
    locale = '',
    onapply,
    onremove,
  }: {
    /** The link being edited ('' for a new one). */
    href?: string;
    locale?: string;
    /** `label` is the text to insert when nothing is selected. */
    onapply: (href: string, label: string) => void;
    onremove: () => void;
  } = $props();

  const uid = $props.id();
  // The form is created each time the popover opens: the initial href is all it needs.
  // svelte-ignore state_referenced_locally
  const itemId = href.startsWith('item:') ? href.slice(5) : '';
  // svelte-ignore state_referenced_locally
  let url = $state(itemId ? '' : href);
  let problem = $state('');
  let picked = $state<ComboOption | null>(itemId ? { value: itemId, label: itemId } : null);

  const hint = (type: string) => types.get(type)?.label ?? type;
  const readable = $derived(types.listable.filter((x) => session.can('content.read', { type: x.name })));

  $effect(() => {
    if (!itemId) return;
    const ctl = new AbortController();
    api
      .get('/items/{id}', { params: { id: itemId }, query: { draft: true, locale: locale || undefined } as never, signal: ctl.signal })
      .then((v) => (picked = { value: itemId, label: v.name || itemId, hint: hint(v.type) }))
      .catch(() => {});
    return () => ctl.abort();
  });

  async function search(q: string, signal: AbortSignal): Promise<ComboOption[]> {
    const per = await Promise.all(
      readable.map((ty) =>
        api
          .get('/collections/{type}/items', { params: { type: ty.name }, query: { q, limit: 5, locale: locale || undefined } as never, signal })
          .then((r) => r.items.map((i) => ({ value: i.id, label: i.name || i.id, hint: hint(i.type) })))
          .catch(() => [] as ComboOption[]),
      ),
    );
    return per.flat();
  }

  function submit(e: Event) {
    e.preventDefault();
    const h = normalizeHref(url);
    if (!validHref(h)) {
      problem = t('richtext.linkBad');
      return;
    }
    onapply(h, h.replace(/^(mailto:|https?:\/\/)/, ''));
  }
</script>

<div class="h-rte__link">
  <form onsubmit={submit} novalidate>
    <label class="h-field__label" for="{uid}-u">{t('richtext.linkUrl')}</label>
    <Input
      id="{uid}-u"
      bind:value={url}
      placeholder={t('richtext.linkUrlHint')}
      invalid={!!problem}
      aria-describedby={problem ? `${uid}-e` : undefined}
      oninput={() => (problem = '')}
    />
    {#if problem}<p class="h-field__error" id="{uid}-e" role="alert">{problem}</p>{/if}
    <div class="h-rte__linkactions">
      {#if href}<Button size="sm" variant="ghost" onclick={onremove}>{t('richtext.linkRemove')}</Button>{/if}
      <span class="h-spacer"></span>
      <Button size="sm" variant="primary" type="submit">{t('richtext.linkApply')}</Button>
    </div>
  </form>
  <div class="h-rte__linkitem">
    <label class="h-field__label" for="{uid}-i">{t('richtext.linkItem')}</label>
    <Combobox id="{uid}-i" value={picked} {search} clearable={false} placeholder={t('richtext.linkItemSearch')} onselect={(o) => o && onapply(`item:${o.value}`, o.label)} />
  </div>
</div>
