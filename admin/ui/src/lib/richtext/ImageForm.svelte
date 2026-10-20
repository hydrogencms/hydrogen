<script lang="ts">
  import { t } from '../i18n';
  import Button from '../ui/Button.svelte';
  import Input from '../ui/Input.svelte';
  import { validImageSrc } from './schema';

  let { onapply }: { onapply: (src: string, alt: string) => void } = $props();

  const uid = $props.id();
  let src = $state('');
  let alt = $state('');
  let problem = $state('');

  function submit(e: Event) {
    e.preventDefault();
    const v = src.trim();
    if (!validImageSrc(v)) {
      problem = t('richtext.imageBad');
      return;
    }
    onapply(v, alt.trim());
  }
</script>

<div class="h-rte__link">
  <form onsubmit={submit} novalidate>
    <label class="h-field__label" for="{uid}-s">{t('richtext.imageUrl')}</label>
    <Input
      id="{uid}-s"
      bind:value={src}
      placeholder={t('richtext.imageUrlHint')}
      invalid={!!problem}
      aria-describedby={problem ? `${uid}-e` : undefined}
      oninput={() => (problem = '')}
    />
    {#if problem}<p class="h-field__error" id="{uid}-e" role="alert">{problem}</p>{/if}
    <label class="h-field__label" for="{uid}-a">{t('richtext.imageAlt')}</label>
    <Input id="{uid}-a" bind:value={alt} />
    <div class="h-rte__linkactions">
      <span class="h-spacer"></span>
      <Button size="sm" variant="primary" type="submit">{t('richtext.imageApply')}</Button>
    </div>
  </form>
</div>
