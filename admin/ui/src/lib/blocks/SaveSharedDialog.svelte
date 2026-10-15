<script lang="ts">
  import Button from '../ui/Button.svelte';
  import Dialog from '../ui/Dialog.svelte';
  import Field from '../ui/Field.svelte';
  import Input from '../ui/Input.svelte';
  import Select from '../ui/Select.svelte';
  import { api } from '../api/client';
  import { errorMessage } from '../errors';
  import { t } from '../i18n';
  import { toast } from '../ui/toast.svelte';
  import { types } from '../types.svelte';
  import { blocksCatalog } from './catalog.svelte';
  import type { BlocksModel } from './model.svelte';
  import { blocksFieldOf, cloneBlock, find, ruleOfField } from './tree';

  let { model }: { model: BlocksModel } = $props();

  const id = $derived(model.sharing);
  const found = $derived(id ? find(model.root, id) : undefined);
  const options = $derived(
    found
      ? blocksCatalog.sharedTo.filter((tn) => {
          const f = blocksFieldOf(types.get(tn)?.fields ?? []);
          return !!f && ruleOfField(f).allow.includes(found.block.type);
        })
      : [],
  );
  let type = $state('');
  let name = $state('');
  let busy = $state(false);
  let error = $state('');
  $effect(() => {
    if (id) {
      type = options[0] ?? '';
      name = '';
      error = '';
    }
  });

  /** The field the item's name is built from (`{title}` in the name pattern), else its first required text field. */
  function nameField(tn: string): string {
    const ty = types.get(tn);
    const m = ty?.namePattern ? /\{(\w+)\}/.exec(ty.namePattern) : null;
    return m?.[1] ?? ty?.fields.find((f) => f.shape.base === 'string' && f.required)?.name ?? 'title';
  }

  async function create() {
    if (!found || !type || !name.trim()) return;
    const bf = blocksFieldOf(types.get(type)?.fields ?? []);
    if (!bf) return;
    busy = true;
    error = '';
    try {
      const copy = cloneBlock(found.block);
      const body = { locale: model.locale || undefined, fields: { [nameField(type)]: name.trim(), [bf.name]: [copy] } };
      const made = (await api.post('/collections/{type}/items', { params: { type }, body: body as never })) as { id: string };
      model.replaceWithShared(found.block.id!, made.id);
      toast.success(t('blocks.saved', { name: name.trim() }));
      model.sharing = null;
    } catch (e) {
      error = errorMessage(e) || t('blocks.saveFailed');
    } finally {
      busy = false;
    }
  }
</script>

<Dialog open={!!id} title={t('blocks.saveTitle')} size="sm" onclose={() => (model.sharing = null)}>
  <form
    class="h-form"
    onsubmit={(e) => {
      e.preventDefault();
      void create();
    }}
  >
    <p>{t('blocks.saveText')}</p>
    {#if options.length > 1}
      <Field label={t('blocks.saveType')}>
        {#snippet children(c)}<Select {...c} bind:value={type} options={options.map((o) => ({ value: o, label: types.get(o)?.label ?? o }))} />{/snippet}
      </Field>
    {/if}
    <Field label={t('blocks.saveName')} required {error}>
      {#snippet children(c)}<Input {...c} bind:value={name} autocomplete="off" />{/snippet}
    </Field>
    <button type="submit" hidden aria-hidden="true" tabindex="-1"></button>
  </form>
  {#snippet footer()}
    <Button onclick={() => (model.sharing = null)}>{t('common.cancel')}</Button>
    <Button variant="primary" loading={busy} disabled={!name.trim() || !type} onclick={() => void create()}>{t('blocks.saveCreate')}</Button>
  {/snippet}
</Dialog>
