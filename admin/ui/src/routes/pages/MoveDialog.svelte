<script lang="ts">
  import { api } from '../../lib/api/client';
  import { t } from '../../lib/i18n';
  import { types } from '../../lib/types.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Select from '../../lib/ui/Select.svelte';
  import Tree from '../../lib/ui/Tree.svelte';
  import type { TreeNode } from '../../lib/ui/tree';
  import { canHold, urlChanges, type PNode, type Placement, type SubNode, type UrlChange } from './logic';
  import type { SiteTree } from './siteTree.svelte';

  let {
    open = $bindable(false),
    node,
    store,
    locales,
    initial,
    ondone,
  }: {
    open?: boolean;
    node: PNode;
    store: SiteTree;
    /** Locales to preview URLs for. */
    locales: string[];
    /** Set by a drag: the target the drop chose. */
    initial?: Placement;
    ondone: (r: { from: string; to: string }) => void;
  } = $props();

  const optionsOf = (type: string) => types.get(type)?.options;
  // svelte-ignore state_referenced_locally
  const oldParent = node.parent ?? '';

  // svelte-ignore state_referenced_locally
  let parentId = $state(initial?.parent ?? oldParent);
  // svelte-ignore state_referenced_locally
  let after = $state(initial ? (initial.after ?? '#first') : '#keep');
  const open0 = new Set<string>();
  // svelte-ignore state_referenced_locally
  for (let p: string | undefined = parentId; p !== undefined && p !== ''; p = store.parentOf(p)) open0.add(p);
  let expanded = $state(open0);
  let busy = $state(false);
  let previewing = $state(false);
  let changes = $state<UrlChange[]>([]);
  let previewError = $state('');
  let ready = $state(false);
  let loadingKids = $state(false);

  function below(id: string): boolean {
    for (let p: string | undefined = id; p !== undefined; p = store.parentOf(p)) if (p === node.id) return true;
    return false;
  }
  const valid = (id: string) => !below(id) && !store.byId.get(id)?.restricted && canHold(store.byId.get(id), node, optionsOf);

  function build(parent: string): TreeNode[] {
    return store
      .siblings(parent)
      .map((id) => store.byId.get(id)!)
      .filter((n) => n.kind === 'page')
      .map((n) => ({
        id: n.id,
        label: n.name,
        icon: 'page',
        hasChildren: n.children > 0,
        disabled: !valid(n.id),
        children: store.kids.has(n.id) ? build(n.id) : undefined,
      }));
  }
  const roots = $derived(build(''));

  async function ensureKids(id: string) {
    const n = store.byId.get(id);
    if (n && n.children > 0 && !store.kids.has(id)) await store.loadChildren(id);
  }
  // svelte-ignore state_referenced_locally
  ensureKids(parentId).then(() => (ready = true));

  const siblings = $derived(store.siblings(parentId).filter((id) => id !== node.id));
  const positions = $derived([
    ...(parentId === oldParent ? [{ value: '#keep', label: t('pages.positionKeep') }] : []),
    { value: '#first', label: t('pages.positionFirst') },
    { value: '#last', label: t('pages.positionLast') },
    ...siblings.map((id) => ({ value: id, label: t('pages.positionAfter', { name: store.byId.get(id)?.name ?? id }) })),
  ]);

  async function choose(tn: TreeNode) {
    if (!valid(tn.id)) {
      toast.info(t('pages.notAllowedHere'));
      return;
    }
    loadingKids = true;
    try {
      await ensureKids(tn.id);
    } catch {
      toast.error(t('app.error'));
      return;
    } finally {
      loadingKids = false;
    }
    parentId = tn.id;
    after = '#last';
  }

  const afterId = $derived<string | null | undefined>(
    after === '#keep' ? undefined : after === '#first' ? null : after === '#last' ? (siblings[siblings.length - 1] ?? null) : after,
  );

  const flatten = (list: import('./logic').PNode[], out: SubNode[] = []) => {
    for (const n of list) {
      out.push({ id: n.id, name: n.name, kind: n.kind, path: n.path, status: n.status });
      if (n.nodes) flatten(n.nodes, out);
    }
    return out;
  };

  let seq = 0;
  $effect(() => {
    const target = parentId;
    if (!ready || target === oldParent) {
      changes = [];
      previewError = '';
      return;
    }
    const mine = ++seq;
    previewing = true;
    previewError = '';
    (async () => {
      const targetNode = store.byId.get(target)!;
      const list = locales.length ? locales : [''];
      const newParent: Record<string, string> = {};
      const subtree: Record<string, SubNode[]> = {};
      const rows = targetNode.item ? ((await api.get('/pages/items/{id}/paths', { params: { id: targetNode.item } })) as unknown as { items: { locale: string; path: string; state: string }[] }).items : [];
      for (const l of list) {
        const row = rows.find((r) => r.state === 'current' && (r.locale === l || !l));
        if (row) newParent[l] = row.path;
        else if (!targetNode.parent && targetNode.path !== undefined && l === store.locale) newParent[l] = targetNode.path || '/';
        const [self, kids] = await Promise.all([
          api.get('/pages/sites/{site}/tree', { params: { site: store.site }, query: { parent: oldParent || undefined, depth: 1, locale: l || undefined } }),
          api.get('/pages/sites/{site}/tree', { params: { site: store.site }, query: { parent: node.id, depth: 10, locale: l || undefined } }),
        ]);
        const me = (self as unknown as { items: PNode[] }).items.find((x) => x.id === node.id);
        subtree[l] = [...(me ? flatten([{ ...me, nodes: undefined }]) : []), ...flatten((kids as unknown as { items: PNode[] }).items)];
      }
      if (mine !== seq) return;
      changes = urlChanges(node.id, newParent, subtree);
    })()
      .catch((e: Error) => {
        if (mine === seq) previewError = e.message;
      })
      .finally(() => {
        if (mine === seq) previewing = false;
      });
  });

  const shown = $derived(changes.slice(0, 40));
  const redirects = $derived(changes.some((c) => c.redirect));

  async function confirm() {
    busy = true;
    try {
      const body: Record<string, unknown> = {};
      if (parentId !== oldParent) body.parent = parentId;
      if (afterId !== undefined) body.after = afterId;
      if (!Object.keys(body).length) {
        open = false;
        return;
      }
      await api.patch('/pages/nodes/{id}', { params: { id: node.id }, body: body as never });
      open = false;
      ondone({ from: oldParent, to: parentId });
    } catch (e) {
      const err = e as Error;
      toast.error(err.message);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog bind:open title={t('pages.moveTitle', { name: node.name })} size="lg">
  <div class="h-stack">
    <div>
      <p class="h-muted" id="pages-move-parent">{t('pages.newParent')}</p>
      <div class="h-pages__chooser">
        <Tree nodes={roots} label={t('pages.newParent')} bind:expanded selected={parentId} onselect={choose}
          loadChildren={async (tn) => {
            await store.loadChildren(tn.id);
            return build(tn.id);
          }} />
      </div>
    </div>
    <Field label={t('pages.position')}>
      {#snippet children(c)}<Select {...c} bind:value={after} options={positions} />{/snippet}
    </Field>
    <section aria-live="polite" class="h-stack">
      <h3 class="h-card__title">{t('pages.urlChanges')}</h3>
      {#if previewing}
        <p class="h-muted">{t('app.loading')}</p>
      {:else if previewError}
        <p class="h-alert" role="alert">{previewError}</p>
      {:else if parentId === oldParent}
        <p class="h-muted">{t('pages.noUrlChange')}</p>
      {:else if !changes.length}
        <p class="h-muted">{t('pages.noUrlChange')}</p>
      {:else}
        <ul class="h-pages__changes">
          {#each shown as c (c.locale + c.id)}
            <li>
              {#if locales.length > 1}<span class="h-badge h-badge--neutral">{c.locale}</span>{/if}
              <span class="h-pages__from">{c.from}</span>
              <span aria-hidden="true">→</span><span class="h-sr">{t('pages.to')}</span>
              <span class="h-pages__to">{c.to}</span>
              {#if c.redirect}<span class="h-muted">{t('pages.redirects')}</span>{/if}
            </li>
          {/each}
        </ul>
        {#if changes.length > shown.length}<p class="h-muted">{t('pages.andMore', { n: changes.length - shown.length })}</p>{/if}
        {#if redirects}<p class="h-muted">{t('pages.redirectNote')}</p>{/if}
      {/if}
    </section>
  </div>
  {#snippet footer()}
    <Button onclick={() => (open = false)}>{t('common.cancel')}</Button>
    <Button variant="primary" loading={busy} disabled={previewing || loadingKids || !valid(parentId)} onclick={() => void confirm()}>{t('pages.move')}</Button>
  {/snippet}
</Dialog>
