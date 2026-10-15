<script lang="ts">
  import { router } from './lib/router.svelte';
  import { t } from './lib/i18n';
  import IconSprite from './lib/ui/IconSprite.svelte';
  import ToastHost from './lib/ui/ToastHost.svelte';
  import Skeleton from './lib/ui/Skeleton.svelte';
  import Button from './lib/ui/Button.svelte';
  import Shell from './Shell.svelte';
  import ReloginDialog from './ReloginDialog.svelte';
  import { reauth } from './lib/reauth.svelte';

  $effect(() => {
    const k = router.route?.title;
    document.title = (k ? `${t(k)} · ` : '') + t('app.name');
  });
</script>

{#snippet view()}
  {#if router.loadError}
    <div class="h-page">
      <p class="h-alert" role="alert">{t('app.error')} <Button size="sm" onclick={() => router.retry()}>{t('app.retry')}</Button></p>
    </div>
  {:else if router.component}
    {#key router.path}
      {@const View = router.component}
      <View params={router.params} />
    {/key}
  {:else}
    <div class="h-page"><Skeleton lines={6} height="1.2rem" /></div>
  {/if}
{/snippet}

<IconSprite />
{#if router.route?.public}
  {@render view()}
{:else}
  <Shell>{@render view()}</Shell>
{/if}
<ReloginDialog />
<ToastHost />
