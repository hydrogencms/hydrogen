<script lang="ts">
  import { api, type Schemas } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import { types } from '../../lib/types.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Combobox, { type ComboOption } from '../../lib/ui/Combobox.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Switch from '../../lib/ui/Switch.svelte';
  import Textarea from '../../lib/ui/Textarea.svelte';
  import { filterText, parseFilter, type PNode } from './logic';

  let {
    open = $bindable(false),
    mode,
    parent,
    node,
    locale,
    childTypes = [],
    lastChild,
    ondone,
  }: {
    open?: boolean;
    /** child: new page below `parent`; mount/link: new node below `parent`; mount-edit: change `node`. */
    mode: 'child' | 'mount' | 'link' | 'mount-edit';
    parent: PNode;
    node?: PNode;
    locale: string;
    childTypes?: string[];
    /** Resolves the id of the parent's last child (new nodes go last). */
    lastChild: (parent: string) => Promise<string | undefined>;
    /** `select` is the id of the node to show next. */
    ondone: (r: { parent: string; select?: string }) => void;
  } = $props();

  // svelte-ignore state_referenced_locally
  const mountOf = node?.mount;
  const form = new Form(
    {
    // svelte-ignore state_referenced_locally
    type: childTypes.includes('page') ? 'page' : (childTypes[0] ?? ''),
    name: '',
    segment: mountOf?.segment ?? '',
    collection: mountOf?.collection ?? types.collections[0]?.name ?? '',
    route: mountOf?.route ?? '{slug}',
    canonical: mountOf?.canonical ?? false,
    filter: filterText(mountOf?.filter),
    // svelte-ignore state_referenced_locally
    label: mountOf?.label?.[locale] ?? '',
    target: 'page' as 'page' | 'url',
    url: '',
  },
    { prefix: 'fields.' },
  );
  // A created page whose placement failed: "Create" then only retries the placement.
  let createdId = $state('');
  let placeError = $state('');
  let discarding = $state(false);
  let picked = $state<ComboOption | null>(null);

  const title = $derived(
    mode === 'child' ? t('pages.addChild') : mode === 'mount' ? t('pages.addMount') : mode === 'link' ? t('pages.addLink') : t('pages.editMount'),
  );

  /** The field that holds a new page's name: title or name, else the first required string. */
  function nameField(type: string): string | undefined {
    const fs = types.get(type)?.fields ?? [];
    return (fs.find((f) => f.name === 'title') ?? fs.find((f) => f.name === 'name') ?? fs.find((f) => f.required && f.shape.base === 'string'))?.name;
  }

  async function searchPages(q: string, signal: AbortSignal): Promise<ComboOption[]> {
    const lists = await Promise.all(
      types.pageTypes.map((p) => api.get('/collections/{type}/items', { params: { type: p.name }, query: { q, limit: 8, locale: locale || undefined }, signal })),
    );
    return lists.flatMap((l) => l.items).map((i: Schemas['Summary']) => ({ value: i.id, label: i.name, hint: i.type }));
  }

  const labelMap = (existing?: Record<string, string>, text?: string) => {
    const m = { ...(existing ?? {}) };
    const key = locale || 'en';
    if (text) m[key] = text;
    else delete m[key];
    return m;
  };

  async function submit(v: typeof form.values): Promise<string | undefined> {
    const after = (await lastChild(parent.id)) ?? undefined;
    if (mode === 'child') {
      let id = createdId;
      if (!id) {
        const field = nameField(v.type);
        const created = await api.post('/collections/{type}/items', {
          params: { type: v.type },
          body: { locale: locale || undefined, fields: field ? { [field]: v.name } : {} },
        });
        id = (created as { id: string }).id;
        createdId = id;
      }
      try {
        const placed = await api.post('/pages/sites/{site}/nodes', {
          params: { site: parent.site },
          body: { kind: 'page', item: id, parent: parent.id, after } as never,
        });
        createdId = '';
        placeError = '';
        return (placed as { id: string }).id;
      } catch (e) {
        placeError = t('pages.createdNotPlaced', { id, reason: (e as Error).message });
        throw e;
      }
    }
    if (mode === 'mount' || mode === 'mount-edit') {
      const spec = {
        segment: v.segment.trim(),
        collection: v.collection,
        route: v.route.trim(),
        canonical: v.canonical,
        filter: parseFilter(v.filter),
        label: labelMap(mountOf?.label, v.label.trim()),
      };
      if (mode === 'mount-edit') {
        await api.patch('/pages/nodes/{id}', { params: { id: node!.id }, body: { mount: spec } as never });
        return node!.id;
      }
      const made = await api.post('/pages/sites/{site}/nodes', {
        params: { site: parent.site },
        body: { kind: 'mount', parent: parent.id, after, ...spec } as never,
      });
      return (made as { id: string }).id;
    }
    const target = v.target === 'url' ? { url: v.url.trim() } : { item: picked?.value };
    const made = await api.post('/pages/sites/{site}/nodes', {
      params: { site: parent.site },
      body: { kind: 'link', parent: parent.id, after, label: labelMap({}, v.label.trim()), ...target } as never,
    });
    return (made as { id: string }).id;
  }

  async function discardDraft() {
    discarding = true;
    try {
      await api.del('/items/{id}', { params: { id: createdId } });
      createdId = '';
      placeError = '';
    } catch (e) {
      placeError = (e as Error).message;
    } finally {
      discarding = false;
    }
  }

  // Errors the dialog has no input for are listed above the form.
  const shown = $derived(
    new Set(['type', 'name', nameField(form.values.type) ?? '', 'label', 'url', 'item', 'segment', 'collection', 'route', 'filter']),
  );
  const unrouted = $derived(Object.entries(form.errors).filter(([k]) => !shown.has(k)));

  async function save() {
    let id: string | undefined;
    const ok = await form.submit(async (v) => {
      id = await submit(v);
    });
    if (ok) {
      open = false;
      ondone({ parent: mode === 'mount-edit' ? (node!.parent ?? '') : parent.id, select: id });
    }
  }
</script>

<Dialog bind:open {title} size="md">
  {#if unrouted.length}
    <ul class="h-alert h-plain" role="alert">
      {#each unrouted as [k, m] (k)}<li>{k}: {m}</li>{/each}
    </ul>
  {/if}
  {#if placeError}
    <p class="h-alert" role="alert">
      {placeError}
      {#if createdId}<Button size="sm" loading={discarding} onclick={discardDraft}>{t('pages.discardDraft')}</Button>{/if}
    </p>
  {/if}
  <form
    class="h-stack"
    id="pages-add-form"
    onsubmit={(e) => {
      e.preventDefault();
      void save();
    }}
  >
    {#if mode === 'child'}
      <Field label={t('pages.type')} error={form.error('type')} required>
        {#snippet children(c)}
          <Select {...c} bind:value={form.values.type} options={childTypes.map((n) => ({ value: n, label: types.get(n)?.label ?? n }))} />
        {/snippet}
      </Field>
      <Field label={t('pages.name')} error={form.error('name') ?? form.error(nameField(form.values.type) ?? '')} required>
        {#snippet children(c)}
          <Input {...c} bind:value={form.values.name} required />
        {/snippet}
      </Field>
    {:else if mode === 'link'}
      <Field label={t('pages.label')} error={form.error('label')} required>
        {#snippet children(c)}<Input {...c} bind:value={form.values.label} required />{/snippet}
      </Field>
      <Field label={t('pages.linkTarget')}>
        {#snippet children(c)}
          <Select
            {...c}
            bind:value={form.values.target}
            options={[
              { value: 'page', label: t('pages.targetPage') },
              { value: 'url', label: t('pages.targetUrl') },
            ]}
          />
        {/snippet}
      </Field>
      {#if form.values.target === 'url'}
        <Field label={t('pages.url')} error={form.error('url')} required>
          {#snippet children(c)}<Input {...c} bind:value={form.values.url} placeholder="https://" required />{/snippet}
        </Field>
      {:else}
        <Field label={t('pages.targetPage')} error={form.error('item')} required>
          {#snippet children(c)}<Combobox {...c} bind:value={picked} search={searchPages} placeholder={t('pages.searchPages')} />{/snippet}
        </Field>
      {/if}
    {:else}
      <Field label={t('pages.segment')} description={t('pages.segmentHelp')} error={form.error('segment')} required>
        {#snippet children(c)}<Input {...c} bind:value={form.values.segment} required />{/snippet}
      </Field>
      <Field label={t('pages.collection')} error={form.error('collection')} required>
        {#snippet children(c)}
          <Select {...c} bind:value={form.values.collection} options={types.collections.map((x) => ({ value: x.name, label: x.label }))} />
        {/snippet}
      </Field>
      <Field label={t('pages.route')} description={t('pages.routeHelp')} error={form.error('route')} required>
        {#snippet children(c)}<Input {...c} bind:value={form.values.route} required />{/snippet}
      </Field>
      <Field label={t('pages.filter')} description={t('pages.filterHelp')} error={form.error('filter')}>
        {#snippet children(c)}<Textarea {...c} rows={3} bind:value={form.values.filter} />{/snippet}
      </Field>
      <Field label={t('pages.label')}>
        {#snippet children(c)}<Input {...c} bind:value={form.values.label} />{/snippet}
      </Field>
      <Switch bind:checked={form.values.canonical} label={t('pages.canonical')} />
    {/if}
    {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
  </form>
  {#snippet footer()}
    <Button onclick={() => (open = false)}>{t('common.cancel')}</Button>
    <Button type="submit" variant="primary" form="pages-add-form" loading={form.pending} disabled={mode === 'child' && !childTypes.length}>
      {mode === 'mount-edit' ? t('common.save') : t('pages.create')}
    </Button>
  {/snippet}
</Dialog>
