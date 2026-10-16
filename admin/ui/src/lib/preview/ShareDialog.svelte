<script lang="ts">
  import { tick } from 'svelte';
  import '../messages/preview';
  import { ApiError } from '../api/client';
  import { copyText, errorMessage, reportError } from '../errors';
  import { t } from '../i18n';
  import Badge from '../ui/Badge.svelte';
  import Button from '../ui/Button.svelte';
  import Dialog from '../ui/Dialog.svelte';
  import Field from '../ui/Field.svelte';
  import Input from '../ui/Input.svelte';
  import Select from '../ui/Select.svelte';
  import Time from '../ui/Time.svelte';
  import { toast } from '../ui/toast.svelte';
  import { DEFAULT_EXPIRY, EXPIRIES, shareApi, shareState, type Share } from './share';
  import type { ShareList } from './shares.svelte';

  let {
    open = $bindable(false),
    item,
    channel,
    locale,
    shares,
  }: {
    open?: boolean;
    item: string;
    /** The pane's current channel. */
    channel: string;
    locale: string;
    shares: ShareList;
  } = $props();

  let ttl = $state(DEFAULT_EXPIRY);
  let busy = $state(false);
  let error = $state('');
  // The token is shown once: it is dropped when the dialog closes.
  let link = $state('');
  let input = $state<HTMLInputElement | null>(null);

  $effect(() => {
    if (open) return;
    link = '';
    error = '';
  });

  // The list is read when the dialog opens and every few seconds after (viewers come and go).
  $effect(() => {
    if (!open) return;
    const id = item;
    const load = () => shares.load(id).catch(() => {});
    void load();
    const iv = setInterval(() => !document.hidden && void load(), 5000);
    const show = () => !document.hidden && void load();
    document.addEventListener('visibilitychange', show);
    return () => {
      clearInterval(iv);
      document.removeEventListener('visibilitychange', show);
    };
  });

  async function create() {
    busy = true;
    error = '';
    try {
      const r = await shareApi.create({ item, channel, ...(locale ? { locale } : {}), ttl: Number(ttl) });
      if (r.url) {
        link = new URL(r.url, location.href).href;
        // The button was disabled while creating, which drops the focus: put it on the new link, selected for copying.
        await tick();
        input?.focus();
        input?.select();
      }
      else {
        // Nothing to open without a preview URL: take the link back at once.
        await shareApi.revoke(r.id).catch(() => {});
        error = t('preview.noUrl');
      }
      void shares.load(item).catch(() => {});
    } catch (e) {
      error = e instanceof ApiError && e.status === 403 ? t('preview.share.off') : e instanceof ApiError ? errorMessage(e) : t('preview.share.failed');
    } finally {
      busy = false;
    }
  }

  async function copy() {
    if (await copyText(link)) toast.success(t('preview.share.copied'));
    else {
      input?.select();
      toast.error(t('preview.share.copyFailed'));
    }
  }

  async function revoke(s: Share) {
    if (!confirm(t('preview.share.revokeConfirm'))) return;
    try {
      await shareApi.revoke(s.id);
      toast.success(t('preview.share.revokedToast'));
    } catch (e) {
      reportError(e);
    }
    void shares.load(item).catch(() => {});
  }

  const who = (s: Share) => s.createdByName || s.createdBy;
  const tones = { active: 'success', expired: 'neutral', revoked: 'danger' } as const;
  const rows = $derived(shares.items.map((s) => ({ s, state: shareState(s) })));
</script>

<Dialog bind:open title={t('preview.share.title')} size="lg" dismissible={!busy}>
  <div class="h-stack h-share">
    <p>{t('preview.share.text')}</p>
    <div class="h-share__form">
      <span class="h-muted">{t('preview.share.channel')}: <strong>{channel}</strong>{locale ? ` · ${locale}` : ''}</span>
      <Field label={t('preview.share.expiry')}>
        {#snippet children(c)}
          <Select {...c} bind:value={ttl} options={EXPIRIES.map((e) => ({ value: e.value, label: t(e.key) }))} />
        {/snippet}
      </Field>
      <Button variant="primary" loading={busy} onclick={() => void create()}>{t('preview.share.create')}</Button>
    </div>
    {#if error}<p class="h-alert" role="alert">{error}</p>{/if}
    {#if link}
      <div class="h-share__link">
        <Input bind:el={input} readonly value={link} aria-label={t('preview.share.linkLabel')} onfocus={(e) => (e.currentTarget as HTMLInputElement).select()} />
        <Button icon="copy" onclick={() => void copy()}>{t('preview.share.copy')}</Button>
      </div>
      <p class="h-muted">{t('preview.share.once')}</p>
    {/if}
    {#if rows.length === 0}
      <p class="h-muted">{t('preview.share.none')}</p>
    {:else}
    <ul class="h-share__list" role="list" aria-label={t('preview.share.list')}>
      {#each rows as { s, state } (s.id)}
        <li class="h-share__row">
          <div class="h-share__main">
            <span><strong>{s.channel}</strong>{s.locale ? ` · ${s.locale}` : ''}</span>
            <span class="h-muted">{t('preview.share.by', { name: who(s) })}</span>
            <span class="h-muted">
              {#if state === 'revoked'}{t('preview.share.revokedAt')} <Time at={s.revokedAt} />
              {:else}{t(state === 'active' ? 'preview.share.expires' : 'preview.share.expired')} <Time at={s.expiresAt} />{/if}
            </span>
            {#if state === 'active'}<span class="h-muted h-num">{t('preview.share.viewers', { n: s.viewers })}</span>{/if}
          </div>
          <Badge tone={tones[state]} dot>{t(`preview.share.state.${state}`)}</Badge>
          {#if state === 'active'}<Button size="sm" variant="danger" aria-label={t('preview.share.revokeLabel', { what: `${s.channel}${s.locale ? ` · ${s.locale}` : ''}`, name: who(s) })} onclick={() => void revoke(s)}>{t('preview.share.revoke')}</Button>{/if}
        </li>
      {/each}
    </ul>
    {/if}
  </div>
</Dialog>

<style>
  .h-share__form {
    display: flex;
    align-items: flex-end;
    flex-wrap: wrap;
    gap: var(--h-space-3);
  }
  .h-share__link {
    display: flex;
    gap: var(--h-space-2);
  }
  .h-share__link :global(.h-input) {
    flex: 1;
    min-width: 0;
  }
  .h-share__list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
  }
  .h-share__row {
    display: flex;
    align-items: center;
    gap: var(--h-space-3);
    padding: var(--h-space-2) var(--h-space-3);
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius);
  }
  .h-share__main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-wrap: wrap;
    gap: var(--h-space-1) var(--h-space-3);
    font-size: var(--h-text-sm);
  }
</style>
