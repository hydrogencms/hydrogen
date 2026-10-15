<script lang="ts">
  import { api, type Schemas } from '../../lib/api/client';
  import { t } from '../../lib/i18n';
  import { formatDateTime } from '../../lib/format';
  import { link, href } from '../../lib/router.svelte';
  import Badge from '../../lib/ui/Badge.svelte';
  import Button from '../../lib/ui/Button.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Pagination from '../../lib/ui/Pagination.svelte';
  import Table from '../../lib/ui/Table.svelte';
  import { filterText, mountQuery, type PNode } from './logic';

  let { node, locale, languages, canEdit, onedit }: {
    node: PNode;
    locale: string;
    languages: string[];
    canEdit: boolean;
    onedit: () => void;
  } = $props();

  const PAGE = 20;
  let page = $state(1);
  let q = $state('');
  let rows = $state<Schemas['Summary'][]>([]);
  let total = $state(0);
  let loading = $state(true);
  let error = $state('');
  let seq = 0;

  async function load() {
    if (!node.mount) return;
    const mine = ++seq;
    loading = true;
    error = '';
    try {
      const { type, query } = mountQuery(node.mount, { limit: PAGE, offset: (page - 1) * PAGE }, locale);
      if (q.trim()) query.q = q.trim();
      const res = await api.get('/collections/{type}/items', { params: { type }, query: query as never });
      if (mine !== seq) return;
      rows = res.items;
      total = res.total;
    } catch (e) {
      if (mine === seq) error = (e as Error).message;
    } finally {
      if (mine === seq) loading = false;
    }
  }
  $effect(() => {
    void node.id;
    void page;
    void locale;
    void load();
  });

  let timer: ReturnType<typeof setTimeout>;
  function typing() {
    clearTimeout(timer);
    timer = setTimeout(() => {
      if (page === 1) void load();
      else page = 1;
    }, 250);
  }
  const tone = (s: string) => (s === 'published' ? 'success' : s === 'changed' ? 'warning' : 'neutral');
</script>

<div class="h-stack h-pages__node">
  <div class="h-page__head">
    <div class="h-row">
      <h2 class="h-pages__nodetitle">{node.name}</h2>
      <Badge>{t('pages.kind.mount')}</Badge>
    </div>
    <span class="h-spacer"></span>
    {#if canEdit}<Button size="sm" icon="edit" onclick={onedit}>{t('pages.editMount')}</Button>{/if}
  </div>
  <dl class="h-pages__facts">
    <dt>{t('pages.collection')}</dt><dd>{node.mount?.collection}</dd>
    <dt>{t('pages.route')}</dt><dd><code>{node.path ? node.path + '/' : ''}{node.mount?.route}</code></dd>
    {#if node.mount?.filter}<dt>{t('pages.filter')}</dt><dd><code>{filterText(node.mount.filter).replaceAll('\n', ' · ')}</code></dd>{/if}
  </dl>
  <Input type="search" aria-label={t('common.search')} placeholder={t('common.search')} bind:value={q} oninput={typing} />
  {#if error}
    <p class="h-alert" role="alert">{error} <Button size="sm" onclick={() => void load()}>{t('app.retry')}</Button></p>
  {/if}
  <Table
    caption={t('pages.mountedItems', { name: node.name })}
    {loading}
    {rows}
    rowKey={(r) => r.id}
    columns={[
      { key: 'name', label: t('pages.name'), cell: nameCell },
      { key: 'status', label: t('pages.status'), cell: statusCell },
      { key: 'locales', label: t('pages.languages'), cell: langCell },
      { key: 'updatedAt', label: t('pages.updated'), cell: updCell },
    ]}
  >
    {#snippet empty()}<p class="h-muted">{t('pages.noMounted')}</p>{/snippet}
  </Table>
  <Pagination {page} pageSize={PAGE} {total} onpage={(p) => (page = p)} />
</div>

{#snippet nameCell(r: Schemas['Summary'])}<a use:link href={href('/items/' + r.id)}>{r.name}</a>{/snippet}
{#snippet statusCell(r: Schemas['Summary'])}<Badge tone={tone(r.status)}>{t('pages.status.' + r.status)}</Badge>{/snippet}
{#snippet langCell(r: Schemas['Summary'])}{languages.filter((l) => r.locales.includes(l)).join(', ') || r.locales.join(', ')}{/snippet}
{#snippet updCell(r: Schemas['Summary'])}{formatDateTime(r.updatedAt)}{/snippet}

<style>
  .h-pages__node {
    padding: var(--h-space-4);
  }
  .h-pages__nodetitle {
    font-size: var(--h-text-2xl);
    font-weight: 650;
    letter-spacing: -0.02em;
  }
  .h-pages__facts {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--h-space-1) var(--h-space-3);
    margin: 0;
  }
  .h-pages__facts dt {
    color: var(--h-text-muted, inherit);
  }
  .h-pages__facts dd {
    margin: 0;
  }
</style>
