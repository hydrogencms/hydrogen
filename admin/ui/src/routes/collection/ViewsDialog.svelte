<script lang="ts">
  import { session } from '../../lib/auth/session.svelte';
  import { t } from '../../lib/i18n';
  import Button from '../../lib/ui/Button.svelte';
  import Dialog from '../../lib/ui/Dialog.svelte';
  import Field from '../../lib/ui/Field.svelte';
  import IconButton from '../../lib/ui/IconButton.svelte';
  import Input from '../../lib/ui/Input.svelte';
  import { parseViews, upsertView, type SavedView } from './query';

  let {
    open = $bindable(false),
    type,
    current,
    onapply,
  }: { open?: boolean; type: string; current: string; onapply: (query: string) => void } = $props();

  const storageKey = $derived(`h.views.${session.principalKey}.${type}`);
  let views = $state<SavedView[]>([]);
  let name = $state('');

  $effect(() => {
    if (open) {
      views = parseViews(localStorage.getItem(storageKey));
      name = '';
    }
  });

  function persist(next: SavedView[]) {
    views = next;
    localStorage.setItem(storageKey, JSON.stringify(next));
  }
  function save() {
    const n = name.trim();
    if (!n) return;
    persist(upsertView(views, { name: n, query: current }));
    name = '';
  }
  function apply(v: SavedView) {
    open = false;
    onapply(v.query);
  }
</script>

<Dialog bind:open title={t('collections.views')}>
  <form
    class="h-views__save"
    onsubmit={(e) => {
      e.preventDefault();
      save();
    }}
  >
    <Field label={t('collections.viewName')} description={t('collections.viewNameHelp')}>
      {#snippet children(c)}<Input {...c} bind:value={name} />{/snippet}
    </Field>
    <Button type="submit" variant="primary" disabled={!name.trim()}>{t('collections.saveView')}</Button>
  </form>
  {#if views.length === 0}
    <p class="h-muted">{t('collections.noViews')}</p>
  {:else}
    <ul class="h-list">
      {#each views as v (v.name)}
        <li class="h-list__item">
          <Button variant="ghost" onclick={() => apply(v)}>{v.name}</Button>
          <IconButton icon="trash" label={t('collections.deleteView', { name: v.name })} onclick={() => persist(views.filter((x) => x.name !== v.name))} />
        </li>
      {/each}
    </ul>
  {/if}
</Dialog>

<style>
  .h-views__save {
    display: flex;
    align-items: flex-end;
    gap: var(--h-space-3);
    margin-bottom: var(--h-space-4);
  }
  .h-views__save > :global(.h-field) {
    flex: 1;
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }
</style>
