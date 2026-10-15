<script lang="ts">
  import { api } from '../../lib/api/client';
  import { Form } from '../../lib/form.svelte';
  import { t } from '../../lib/i18n';
  import Button from '../../lib/ui/Button.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import Skeleton from '../../lib/ui/Skeleton.svelte';
  import { toast } from '../../lib/ui/toast.svelte';
  import type { LinkSpec, PNode } from './logic';

  let { node, languages, canEdit, onsaved }: { node: PNode; languages: string[]; canEdit: boolean; onsaved: () => void } = $props();

  const form = new Form({ labels: {} as Record<string, string>, url: '' });
  form.guard();
  let spec = $state<LinkSpec | null>(null);
  let error = $state('');
  const langs = $derived(languages.length ? languages : ['en']);

  $effect(() => {
    const id = node.id;
    spec = null;
    error = '';
    api
      .get('/pages/nodes/{id}', { params: { id } })
      .then((n) => {
        const l = (n as unknown as PNode).link!;
        spec = l;
        const labels: Record<string, string> = {};
        for (const k of langs) labels[k] = l.label[k] ?? '';
        form.reset({ labels, url: l.url ?? '' });
      })
      .catch((e: Error) => (error = e.message));
  });

  async function save() {
    if (!spec) return;
    const ok = await form.submit(async (v) => {
      const label: Record<string, string> = { ...spec!.label };
      for (const [k, x] of Object.entries(v.labels)) {
        if (x.trim()) label[k] = x.trim();
        else delete label[k];
      }
      const next: LinkSpec = spec!.url !== undefined ? { label, url: v.url.trim() } : spec!.item ? { label, item: spec!.item } : { label, node: spec!.node };
      await api.patch('/pages/nodes/{id}', { params: { id: node.id }, body: { link: next } as never });
    });
    if (ok) {
      toast.success(t('pages.saved'));
      onsaved();
    }
  }
</script>

<div class="h-stack">
  <h2 class="h-card__title">{node.name}</h2>
  {#if error}
    <p class="h-alert" role="alert">{error}</p>
  {:else if !spec}
    <Skeleton lines={3} />
  {:else}
    <form class="h-stack" onsubmit={(e) => { e.preventDefault(); void save(); }}>
      {#each langs as l (l)}
        <Field label={langs.length > 1 ? t('pages.labelIn', { locale: l }) : t('pages.label')} error={form.error('label')}>
          {#snippet children(c)}<Input {...c} bind:value={form.values.labels[l]} disabled={!canEdit} />{/snippet}
        </Field>
      {/each}
      {#if spec.url !== undefined}
        <Field label={t('pages.url')} error={form.error('url')}>
          {#snippet children(c)}<Input {...c} bind:value={form.values.url} disabled={!canEdit} />{/snippet}
        </Field>
      {:else}
        <p class="h-muted">{t('pages.linksTo')} <code>{node.target ?? ''}</code>{#if node.broken} · {t('pages.broken')}{/if}</p>
      {/if}
      {#if form.formError}<p class="h-alert" role="alert">{form.formError}</p>{/if}
      {#if canEdit}<div><Button type="submit" variant="primary" loading={form.pending} disabled={!form.dirty}>{t('common.save')}</Button></div>{/if}
    </form>
  {/if}
</div>
