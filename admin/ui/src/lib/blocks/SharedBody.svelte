<script lang="ts">
  import Badge from '../ui/Badge.svelte';
  import Button from '../ui/Button.svelte';
  import Skeleton from '../ui/Skeleton.svelte';
  import Icon from '../ui/Icon.svelte';
  import { t } from '../i18n';
  import { href, link } from '../router.svelte';
  import { itemStatusTone } from '../status';
  import { blocksCatalog } from './catalog.svelte';
  import type { BlocksModel } from './model.svelte';
  import type { Block } from './tree';

  let { model, block, errCount = 0 }: { model: BlocksModel; block: Block; errCount?: number } = $props();

  const item = $derived(typeof block.props?.item === 'string' ? block.props.item : '');
  const info = $derived(item ? model.shared(item) : undefined);
</script>

<div class="h-shared">
  {#if !info}
    <p class="h-alert" role="alert">{t('blocks.sharedMissing')}</p>
  {:else}
    {#await info}
      <Skeleton lines={2} />
    {:then s}
      <div class="h-shared__head">
        <div class="h-shared__title">
          <Icon name="link" size={16} />
          <strong>{s.name}</strong>
          <Badge tone={itemStatusTone(s.status)}>{t(`collections.status.${s.status}`)}</Badge>
        </div>
        {#if !model.readonly}
          <div class="h-shared__actions">
            <a class="h-button h-button--secondary h-button--sm" use:link href={href(`/items/${s.id}`)}>{t('blocks.sharedEdit')}</a>
            <Button size="sm" onclick={() => void model.detach(block.id!)}>{t('blocks.sharedDetach')}</Button>
          </div>
        {/if}
      </div>
      <p class="h-muted">{t('blocks.sharedHint')}</p>
      {#if s.blocks.length}
        <ul class="h-shared__list" aria-label={s.name}>
          {#each s.blocks as b, i (b.id ?? i)}
            <li><span class="h-block__tile"><Icon name={blocksCatalog.icon(b.type)} size={14} /></span>{blocksCatalog.label(b.type)}</li>
          {/each}
        </ul>
      {:else}
        <p class="h-muted">{t('blocks.sharedEmpty')}</p>
      {/if}
    {:catch}
      <p class="h-alert" role="alert">{t('blocks.sharedMissing')}</p>
    {/await}
  {/if}
  {#if errCount > 0}<p class="h-field__error" role="alert"><Icon name="alert-circle" size={14} />{t('blocks.blockErrors')}</p>{/if}
</div>
