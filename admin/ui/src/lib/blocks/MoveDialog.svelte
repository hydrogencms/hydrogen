<script lang="ts">
  import Button from '../ui/Button.svelte';
  import Dialog from '../ui/Dialog.svelte';
  import { t } from '../i18n';
  import { blocksCatalog } from './catalog.svelte';
  import type { BlocksModel } from './model.svelte';
  import { find, targets } from './tree';

  let { model }: { model: BlocksModel } = $props();

  const id = $derived(model.moving);
  const found = $derived(id ? find(model.root, id) : undefined);
  const label = $derived(found ? blocksCatalog.label(found.block.type) : '');
  const options = $derived(id ? targets(model.schema, model.root, id, model.label) : []);
  let pick = $state('0');
  $effect(() => {
    if (id) pick = '0';
  });
  const open = $derived(!!id);
  const uid = $props.id();

  function go() {
    const o = options[Number(pick)];
    if (!o || !id) return;
    model.moveToContainer(id, o.container, undefined, o.trail.join(' › '));
    model.moving = null;
  }
</script>

<Dialog {open} title={t('blocks.moveTitle', { label })} size="sm" onclose={() => (model.moving = null)}>
  <p>{t('blocks.moveText', { label })}</p>
  <fieldset class="h-moveopts">
    <legend class="h-sr">{t('blocks.moveTarget')}</legend>
    {#each options as o, i (i)}
      <label class="h-moveopt" for="{uid}-{i}">
        <input type="radio" id="{uid}-{i}" name="{uid}-t" value={String(i)} bind:group={pick} />
        <span>{o.trail.join(' › ')}</span>
        <span class="h-muted">{t('blocks.moveBlocks', { n: o.length })}</span>
      </label>
    {/each}
  </fieldset>
  {#snippet footer()}
    <Button onclick={() => (model.moving = null)}>{t('common.cancel')}</Button>
    <Button variant="primary" disabled={options.length === 0} onclick={go}>{t('blocks.moveButton')}</Button>
  {/snippet}
</Dialog>
