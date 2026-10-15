<script lang="ts">
  import { untrack } from 'svelte';
  import '../messages/preview';
  import { api, ApiError } from '../api/client';
  import { t } from '../i18n';
  import Badge from '../ui/Badge.svelte';
  import Button from '../ui/Button.svelte';
  import EmptyState from '../ui/EmptyState.svelte';
  import IconButton from '../ui/IconButton.svelte';
  import Segmented from '../ui/Segmented.svelte';
  import Select from '../ui/Select.svelte';
  import Skeleton from '../ui/Skeleton.svelte';
  import { readShared } from '../blocks/shared';
  import { PreviewHost, type HostStatus } from './host';

  export interface PreviewChannel {
    name: string;
    languages: string[];
  }

  let {
    item,
    itemType,
    title,
    locale,
    channels,
    doc,
    save,
    saveState = 'idle',
    saveMessage = '',
    paused = false,
    onreload,
    onkeep,
    principal = '',
  }: {
    item: string;
    itemType: string;
    title: string;
    locale: string;
    channels: PreviewChannel[];
    /** The working document: `{fields}` for the edited locale (what the form holds right now). */
    doc: unknown;
    /** Reload mode: saves the draft; true when it is saved. */
    save: () => Promise<boolean>;
    saveState?: 'idle' | 'saving' | 'saved' | 'error';
    /** Why the last autosave did not happen, when it did not. */
    saveMessage?: string;
    /** Autosave is paused because the item changed elsewhere: shows the Reload / Keep editing banner. */
    paused?: boolean;
    onreload?: () => void;
    onkeep?: () => void;
    /** Keys the remembered channel and device per person. */
    principal?: string;
  } = $props();

  const KEY = (k: string) => `h.preview.${k}.${principal}`;
  const WIDTHS = { desktop: 1280, tablet: 768, mobile: 390 } as const;
  type Device = keyof typeof WIDTHS | 'fit';

  let device = $state<Device>((localStorage.getItem(KEY('device')) as Device) || 'fit');
  let chosen = $state(localStorage.getItem(KEY('channel')) ?? '');
  // The remembered channel, else one whose first language is the edited one, else any that serves it.
  const channel = $derived(
    channels.find((c) => c.name === chosen) ?? channels.find((c) => c.languages[0] === locale) ?? channels.find((c) => c.languages.includes(locale)) ?? channels[0],
  );
  $effect(() => localStorage.setItem(KEY('device'), device));
  $effect(() => {
    if (channel) localStorage.setItem(KEY('channel'), channel.name);
  });

  // ---- the preview address: a token for the item in the channel, valid for an hour and renewed before that
  let url = $state('');
  let phase = $state<'minting' | 'ready' | 'nourl' | 'failed' | 'forbidden'>('minting');
  let nonce = $state(0);
  $effect(() => {
    void nonce;
    const ch = channel;
    const loc = locale;
    if (!ch) return;
    const ctl = new AbortController();
    untrack(() => (phase = 'minting'));
    let renew: ReturnType<typeof setTimeout> | undefined;
    api
      .post('/preview-tokens', { body: { channel: ch.name, item, ttl: 3600, ...(ch.languages.includes(loc) ? { locale: loc } : {}) } as never, signal: ctl.signal })
      .then((r) => {
        if (ctl.signal.aborted) return;
        const u = (r as { url?: string }).url;
        if (!u) {
          url = '';
          phase = 'nourl';
          return;
        }
        url = u;
        phase = 'ready';
        renew = setTimeout(() => nonce++, 50 * 60_000);
      })
      .catch((e) => {
        if (ctl.signal.aborted) return;
        phase = e instanceof ApiError && e.status === 403 ? 'forbidden' : 'failed';
      });
    return () => {
      ctl.abort();
      clearTimeout(renew);
    };
  });

  const origin = $derived.by(() => {
    try {
      return url ? new URL(url, location.href).origin : '';
    } catch {
      return '';
    }
  });
  const src = $derived(url ? new URL(url, location.href).href : '');

  // ---- the protocol
  let frame = $state<HTMLIFrameElement>();
  let status = $state<HostStatus>({ phase: 'connecting' });
  let ms = $state<number | null>(null);
  let host = $state.raw<PreviewHost>();
  $effect(() => {
    void nonce;
    if (!origin) return;
    const h = new PreviewHost({
      origin,
      frame: () => frame?.contentWindow ?? null,
      item,
      itemType,
      locale,
      save: () => save(),
      onStatus: (s) => (status = s),
      onRendered: (n) => (ms = n),
    });
    host = h;
    status = { phase: 'connecting' };
    ms = null;
    const onmsg = (e: MessageEvent) => h.handle(e);
    addEventListener('message', onmsg);
    return () => {
      removeEventListener('message', onmsg);
      h.destroy();
      host = undefined;
    };
  });

  // Shared blocks arrive inlined from delivery; the working document has only their reference, so the content
  // of the shared items is added here (read once per item).
  let sharedBlocks = $state<Record<string, unknown[]>>({});
  const sharedIds = $derived.by(() => {
    const ids = new Set<string>();
    const walk = (v: unknown) => {
      if (Array.isArray(v)) v.forEach(walk);
      else if (v && typeof v === 'object') {
        const o = v as { type?: unknown; props?: { item?: unknown } };
        if (o.type === 'shared' && typeof o.props?.item === 'string') ids.add(o.props.item);
        for (const x of Object.values(o)) walk(x);
      }
    };
    walk(doc);
    return [...ids];
  });
  $effect(() => {
    const ids = sharedIds.filter((id) => !(id in untrack(() => sharedBlocks)));
    for (const id of ids) {
      sharedBlocks[id] = [];
      readShared(id, locale).then(
        (s) => (sharedBlocks[id] = s.blocks),
        () => {},
      );
    }
  });
  const working = $derived.by(() => {
    const walk = (v: unknown): unknown => {
      if (Array.isArray(v)) return v.map(walk);
      if (v && typeof v === 'object') {
        const o = v as Record<string, unknown> & { type?: unknown; props?: { item?: unknown } };
        const out: Record<string, unknown> = {};
        for (const [k, x] of Object.entries(o)) out[k] = walk(x);
        if (o.type === 'shared' && typeof o.props?.item === 'string' && o.props.item in sharedBlocks) out.blocks = sharedBlocks[o.props.item];
        return out;
      }
      return v;
    };
    return walk(doc);
  });
  $effect(() => {
    host?.setDoc(working);
  });

  let noticed = $state(sessionStorage.getItem('h.preview.noticed') === '1');
  function dismissNotice() {
    noticed = true;
    sessionStorage.setItem('h.preview.noticed', '1');
  }
  function reload() {
    ms = null;
    status = { phase: 'connecting' };
    nonce++;
  }

  // ---- fitting the frame
  let stage = $state<HTMLElement>();
  let box = $state({ w: 0, h: 0 });
  $effect(() => {
    if (!stage || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(([e]) => (box = { w: e?.contentRect.width ?? 0, h: e?.contentRect.height ?? 0 }));
    ro.observe(stage);
    return () => ro.disconnect();
  });
  const width = $derived(device === 'fit' ? box.w : WIDTHS[device]);
  const scale = $derived(device === 'fit' || !box.w ? 1 : Math.min(1, box.w / width));

  const devices = $derived([
    { value: 'desktop', label: t('preview.desktop'), icon: 'monitor', iconOnly: true },
    { value: 'tablet', label: t('preview.tablet'), icon: 'tablet', iconOnly: true },
    { value: 'mobile', label: t('preview.mobile'), icon: 'smartphone', iconOnly: true },
    { value: 'fit', label: t('preview.fit'), icon: 'maximize', iconOnly: true },
  ]);
  const chip = $derived(
    status.phase === 'live'
      ? { tone: 'success' as const, text: t('preview.status.live') }
      : status.phase === 'reload'
        ? { tone: 'info' as const, text: t('preview.status.reload') }
        : status.phase === 'error'
          ? { tone: 'danger' as const, text: t('preview.status.error') }
          : status.phase === 'disconnected'
            ? { tone: 'warning' as const, text: t('preview.status.disconnected') }
          : { tone: 'neutral' as const, text: t('preview.status.connecting') },
  );
</script>

<div class="h-preview" data-preview-phase={status.phase}>
  <div class="h-preview__bar">
    {#if channels.length > 1}
      <Select compact aria-label={t('preview.channel')} value={channel?.name ?? ''} options={channels.map((c) => ({ value: c.name, label: c.name }))} onchange={(e) => (chosen = (e.currentTarget as HTMLSelectElement).value)} />
    {/if}
    <Segmented label={t('preview.device')} size="sm" options={devices} value={device} onchange={(v) => (device = v as Device)} />
    <span class="h-spacer"></span>
    <IconButton icon="refresh" size="sm" label={t('preview.reload')} onclick={reload} disabled={phase !== 'ready'} />
    {#if src}
      <Button size="sm" icon="external-link" href={src} {...{ target: "_blank", rel: "noopener", "aria-label": t('preview.open'), title: t('preview.open') }} />
    {/if}
  </div>
  <div class="h-preview__status" role="status" aria-live="polite">
    {#if phase === 'ready'}
      <Badge tone={chip.tone} dot>{chip.text}</Badge>
      {#if ms !== null}<span class="h-preview__ms h-num" title={t('preview.latencyLabel', { ms })}>{t('preview.latency', { ms })}</span>{/if}
      {#if status.phase === 'reload'}
        <span class="h-muted h-preview__save" title={t('preview.saveHint')}>
          {#if saveState === 'saving'}{t('preview.saving')}{:else if saveState === 'saved'}{t('preview.saved')}{:else if saveState === 'error'}{saveMessage || t('preview.saveFailed')}{/if}
        </span>
      {/if}
      {#if status.phase === 'error' && status.message}<span class="h-preview__msg">{status.message}</span>{/if}
    {/if}
  </div>
  {#if paused}
    <div class="h-alert h-preview__banner" role="alert">
      <span>{t('preview.conflict')}</span>
      <span class="h-row">
        <Button size="sm" onclick={onreload}>{t('preview.conflictReload')}</Button>
        <Button size="sm" variant="ghost" onclick={onkeep}>{t('preview.conflictKeep')}</Button>
      </span>
    </div>
  {:else if status.phase === 'reload' && !noticed}
    <div class="h-preview__notice" role="note">
      <span>{t('preview.notice')}</span>
      <Button size="sm" variant="ghost" onclick={dismissNotice}>{t('preview.noticeOk')}</Button>
    </div>
  {/if}
  <div class="h-preview__stage" bind:this={stage}>
    {#if channels.length === 0}
      <EmptyState icon="eye" title={t('preview.noUrl')} description={t('preview.noChannel')} />
    {:else if phase === 'minting'}
      <div class="h-preview__wait" role="status" aria-busy="true" aria-label={t('preview.minting')}><Skeleton shape="block" height="12rem" /></div>
    {:else if phase === 'nourl'}
      <EmptyState icon="eye" title={t('preview.noUrl')} description={t('preview.noUrlText', { channel: channel?.name ?? '' })} />
    {:else if phase === 'forbidden'}
      <EmptyState icon="lock" title={t('preview.forbidden')} />
    {:else if phase === 'failed'}
      <EmptyState icon="alert-circle" title={t('preview.failed')}>
        {#snippet actions()}<Button onclick={() => nonce++}>{t('preview.retry')}</Button>{/snippet}
      </EmptyState>
    {:else}
      {#key nonce}
        <div class="h-preview__deviceframe" style:width="{width * scale}px" style:height="{box.h}px">
          <iframe
            bind:this={frame}
            class="h-preview__frame"
            title={t('preview.frame', { title })}
            {src}
            sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
            referrerpolicy="no-referrer"
            style:width="{width}px"
            style:height="{scale ? box.h / scale : box.h}px"
            style:transform="scale({scale})"
            onload={() => host?.frameLoaded()}
          ></iframe>
        </div>
      {/key}
    {/if}
  </div>
</div>

<style>
  .h-preview {
    display: flex;
    flex-direction: column;
    gap: var(--h-space-2);
    height: 100%;
    min-height: 0;
  }
  .h-preview__bar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--h-space-2);
  }
  .h-preview__status {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--h-space-2);
    min-height: 1.5rem;
    font-size: var(--h-text-sm);
  }
  .h-preview__ms {
    padding: 0 var(--h-space-2);
    border-radius: var(--h-radius-pill);
    background: var(--h-surface-2);
    color: var(--h-text-muted);
    font-size: var(--h-text-xs);
    line-height: 1.5rem;
  }
  .h-preview__banner,
  .h-preview__notice {
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: var(--h-space-2);
    font-size: var(--h-text-sm);
  }
  .h-preview__notice {
    padding: var(--h-space-2) var(--h-space-3);
    border-radius: var(--h-radius);
    background: var(--h-info-soft);
    color: var(--h-info);
  }
  .h-preview__msg {
    color: var(--h-danger);
    overflow-wrap: anywhere;
  }
  .h-preview__stage {
    position: relative;
    flex: 1;
    min-height: 20rem;
    overflow: hidden;
    border: 1px solid var(--h-border);
    border-radius: var(--h-radius-md);
    background: var(--h-surface-2);
  }
  .h-preview__wait {
    padding: var(--h-space-4);
  }
  .h-preview__deviceframe {
    margin-inline: auto;
    overflow: hidden;
    background: #fff;
  }
  .h-preview__frame {
    display: block;
    border: 0;
    background: #fff;
    transform-origin: 0 0;
  }
</style>
