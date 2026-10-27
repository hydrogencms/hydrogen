<script lang="ts">
  import { untrack } from 'svelte';
  import { t } from '../../lib/i18n';
  import { types } from '../../lib/types.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import Tree from '../../lib/ui/Tree.svelte';
  import type { TreeNode } from '../../lib/ui/tree';
  import { canHold, defaultParent } from './logic';
  import { SiteTree } from './siteTree.svelte';

  // Picks the page a page item of `type` goes under in `site`. `value` is the parent's node id; '' means none
  // chosen (or, with `empty`, that the item becomes the site's root).
  let {
    site,
    locale,
    type,
    value = $bindable(''),
    empty = $bindable(false),
    label,
  }: {
    site: string;
    locale: string;
    type: string;
    value?: string;
    empty?: boolean;
    label: string;
  } = $props();

  const optionsOf = (n: string) => types.get(n)?.options;
  let store = $state<SiteTree>();
  let expanded = $state(new Set<string>());
  let ready = $state(false);
  let error = $state('');

  const valid = (id: string) => {
    const n = store?.byId.get(id);
    return !!n && !n.restricted && canHold(n, { kind: 'page', type }, optionsOf);
  };

  function build(parent: string): TreeNode[] {
    if (!store) return [];
    return store
      .siblings(parent)
      .map((id) => store!.byId.get(id)!)
      .filter((n) => n.kind === 'page')
      .map((n) => ({
        id: n.id,
        label: n.name || '…',
        icon: 'page',
        hasChildren: n.children > 0,
        disabled: !valid(n.id),
        children: store!.kids.has(n.id) ? build(n.id) : undefined,
      }));
  }
  const roots = $derived(build(''));

  async function load(s: string, l: string) {
    const tree = new SiteTree(s, l);
    store = tree;
    ready = false;
    error = '';
    value = '';
    try {
      await tree.loadChildren('');
    } catch (e) {
      if (store === tree) error = (e as Error).message;
      return;
    }
    if (store !== tree) return;
    const root = tree.kids.get('')?.[0];
    empty = !root;
    if (root) {
      expanded = new Set([root]);
      value = defaultParent(tree.byId.get(root), type, optionsOf);
    }
    ready = true;
  }
  $effect(() => {
    const s = site;
    const l = locale;
    untrack(() => void load(s, l));
    return () => store?.invalidate();
  });

  async function choose(tn: TreeNode) {
    if (!valid(tn.id)) {
      toast.info(t('pages.notAllowedHere'));
      return;
    }
    value = tn.id;
    const n = store?.byId.get(tn.id);
    if (n && n.children > 0 && !store!.kids.has(tn.id)) await store!.loadChildren(tn.id).catch(() => undefined);
  }
</script>

{#if error}
  <p class="h-alert" role="alert">{error}</p>
{:else if !ready}
  <Skeleton lines={3} />
{:else if empty}
  <p class="h-muted">{t('pages.becomesRoot')}</p>
{:else}
  <div class="h-parentpick">
    <Tree
      nodes={roots}
      {label}
      bind:expanded
      selected={value}
      onselect={choose}
      loadChildren={async (tn) => {
        await store!.loadChildren(tn.id);
        return build(tn.id);
      }}
    />
  </div>
  {#if !value}<p class="h-muted">{t('pages.pickParent')}</p>{/if}
{/if}

<style>
  .h-parentpick {
    max-height: 16rem;
    overflow: auto;
    padding: var(--h-space-1);
    border-radius: var(--h-radius);
    background: var(--h-surface-2);
  }
</style>
