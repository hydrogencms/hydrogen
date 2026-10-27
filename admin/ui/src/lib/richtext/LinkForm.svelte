<script lang="ts">
  import { api } from '../api/client';
  import { session } from '../auth/session.svelte';
  import { t } from '../i18n';
  import { types } from '../types.svelte';
  import Button from '../ui/Button.svelte';
  import Checkbox from '../ui/Checkbox.svelte';
  import Combobox, { type ComboOption } from '../ui/Combobox.svelte';
  import Input from '../ui/Input.svelte';
  import Select from '../ui/Select.svelte';
  import { ID_RE, normalizeHref, validHref } from './schema';

  let {
    href = '',
    target = null,
    ids = [],
    locale = '',
    onapply,
    onremove,
  }: {
    /** The link being edited ('' for a new one). */
    href?: string;
    /** `_blank` when the link opens in a new tab. */
    target?: string | null;
    /** The anchors of the document, offered as `#id` links. */
    ids?: string[];
    locale?: string;
    /** `label` is the text to insert when nothing is selected. */
    onapply: (href: string, label: string, target: string | null) => void;
    onremove: () => void;
  } = $props();

  const uid = $props.id();
  // The form is created each time the popover opens: the initial href is all it needs.
  // svelte-ignore state_referenced_locally
  const itemId = href.startsWith('item:') ? href.slice(5).split('#')[0] : '';
  // svelte-ignore state_referenced_locally
  let url = $state(itemId ? '' : href);
  // svelte-ignore state_referenced_locally
  let anchor = $state(itemId ? (href.split('#')[1] ?? '') : '');
  // svelte-ignore state_referenced_locally
  let blank = $state(target === '_blank');
  let problem = $state('');
  let anchorProblem = $state('');
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

  const tgt = () => (blank ? '_blank' : null);
  const fragment = () => {
    const a = anchor.trim().replace(/^#/, '');
    if (a && !ID_RE.test(a)) {
      anchorProblem = t('richtext.anchorBad');
      return null;
    }
    return a ? `#${a}` : '';
  };

  function submit(e: Event) {
    e.preventDefault();
    if (url.trim() === '' && picked) {
      const f = fragment();
      if (f !== null) onapply(`item:${picked.value}${f}`, picked.label, tgt());
      return;
    }
    const h = normalizeHref(url);
    if (!validHref(h)) {
      problem = t('richtext.linkBad');
      return;
    }
    onapply(h, h.replace(/^(mailto:|https?:\/\/)/, ''), tgt());
  }

  function pick(o: ComboOption | null) {
    if (!o) return;
    const f = fragment();
    if (f !== null) onapply(`item:${o.value}${f}`, o.label, tgt());
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
    {#if ids.length}
      <label class="h-field__label" for="{uid}-h">{t('richtext.linkAnchorPage')}</label>
      <Select id="{uid}-h" value={url.startsWith('#') && ids.includes(url.slice(1)) ? url.slice(1) : ''} options={ids.map((i) => ({ value: i, label: `#${i}` }))} placeholder={t('richtext.linkAnchorNone')} onchange={(v) => (url = v ? `#${v}` : '')} />
    {/if}
    <Checkbox label={t('richtext.linkNewTab')} bind:checked={blank} />
    <div class="h-rte__linkactions">
      {#if href}<Button size="sm" variant="ghost" onclick={onremove}>{t('richtext.linkRemove')}</Button>{/if}
      <span class="h-spacer"></span>
      <Button size="sm" variant="primary" type="submit">{t('richtext.linkApply')}</Button>
    </div>
  </form>
  <div class="h-rte__linkitem">
    <label class="h-field__label" for="{uid}-f">{t('richtext.linkAnchorItem')}</label>
    <Input id="{uid}-f" bind:value={anchor} invalid={!!anchorProblem} aria-describedby={anchorProblem ? `${uid}-fe` : undefined} oninput={() => (anchorProblem = '')} />
    {#if anchorProblem}<p class="h-field__error" id="{uid}-fe" role="alert">{anchorProblem}</p>{/if}
    <label class="h-field__label" for="{uid}-i">{t('richtext.linkItem')}</label>
    <Combobox id="{uid}-i" value={picked} {search} clearable={false} placeholder={t('richtext.linkItemSearch')} onselect={pick} />
  </div>
</div>
