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
  import { blocksCatalog } from '../blocks/catalog.svelte';
  import { readShared } from '../blocks/shared';
  import { session } from '../auth/session.svelte';
  import Canvas from '../canvas/Canvas.svelte';
  import type { LayoutNode } from '../canvas/drop';
  import { textRefs } from '../canvas/texts';
  import type { CompiledField } from '../types.svelte';
  import '../messages/canvas';
  import { PreviewHost, type HostStatus } from './host';
  import type { RelayState } from './relay';
  import ShareDialog from './ShareDialog.svelte';
  import type { ShareList } from './shares.svelte';

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
    flush = async () => true,
    relayState = 'idle',
    dirty = false,
    shares,
    onmode,
    principal = '',
    fields = [],
    setField = () => {},
    readonly = false,
  }: {
    item: string;
    itemType: string;
    title: string;
    locale: string;
    channels: PreviewChannel[];
    /** The working document: `{fields}` for the edited locale (what the form holds right now). */
    doc: unknown;
    /** Reload mode: resolves true when the server holds the working document (the relay has sent it). */
    flush?: () => Promise<boolean>;
    relayState?: RelayState;
    /** The form has unsaved changes. */
    dirty?: boolean;
    /** The item's share links (the editor keeps the list: it relays drafts while a link is active). */
    shares?: ShareList;
    /** The frame's protocol mode, so the editor knows when reload mode needs the relay. */
    onmode?: (mode: 'live' | 'reload' | null) => void;
    /** Keys the remembered channel and device per person. */
    principal?: string;
    /** The item type's fields, for the canvas (inline text edits, empty blocks fields). */
    fields?: CompiledField[];
    /** Writes a top-level field of the edited locale into the form. */
    setField?: (name: string, value: unknown) => void;
    readonly?: boolean;
  } = $props();

  // Props like item={data.id} are getters on the editor's whole item, which every save replaces: compare by value,
  // so the token and the protocol host only start over when the item, its type or the locale really change.
  const itemId = $derived(item);
  const typeName = $derived(itemType);
  const lang = $derived(locale);

  const KEY = (k: string) => `h.preview.${k}.${principal}`;
  const WIDTHS = { desktop: 1280, tablet: 768, mobile: 390 } as const;
  type Device = keyof typeof WIDTHS | 'fit';

  let device = $state<Device>((localStorage.getItem(KEY('device')) as Device) || 'fit');
  let chosen = $state(localStorage.getItem(KEY('channel')) ?? '');
  // The remembered channel, else one whose first language is the edited one, else any that serves it.
  const channel = $derived(
    channels.find((c) => c.name === chosen) ?? channels.find((c) => c.languages[0] === locale) ?? channels.find((c) => c.languages.includes(locale)) ?? channels[0],
  );
  let canvasOn = $state(localStorage.getItem(KEY('canvas')) !== '0');
  $effect(() => localStorage.setItem(KEY('device'), device));
  $effect(() => localStorage.setItem(KEY('canvas'), canvasOn ? '1' : '0'));
  $effect(() => {
    if (channel) localStorage.setItem(KEY('channel'), channel.name);
  });

  // ---- the preview address: a token for the item in the channel, valid for an hour and renewed before that
  let url = $state('');
  let phase = $state<'minting' | 'ready' | 'nourl' | 'failed' | 'forbidden'>('minting');
  let nonce = $state(0);
  // Keyed by plain values: the channel list is rebuilt whenever the editor reloads the item (each autosave), and a
  // new token would reload the frame.
  const chName = $derived(channel?.name ?? '');
  const chLocale = $derived(channel?.languages.includes(lang) ? lang : '');
  $effect(() => {
    void nonce;
    const name = chName;
    const loc = chLocale;
    if (!name) return;
    const ctl = new AbortController();
    untrack(() => (phase = 'minting'));
    let renew: ReturnType<typeof setTimeout> | undefined;
    api
      .post('/preview-tokens', { body: { channel: name, item: itemId, ttl: 3600, ...(loc ? { locale: loc } : {}) } as never, signal: ctl.signal })
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
  // A preview on the admin's own origin runs sandboxed without allow-same-origin (opaque origin, no handle on the admin).
  const locked = $derived(!!origin && origin === location.origin);
  const token = $derived.by(() => {
    try {
      return src ? (new URL(src).searchParams.get('preview') ?? '') : '';
    } catch {
      return '';
    }
  });

  // ---- the protocol
  let frame = $state<HTMLIFrameElement>();
  let status = $state<HostStatus>({ phase: 'connecting' });
  let ms = $state<number | null>(null);
  let host = $state.raw<PreviewHost>();
  // ---- the canvas: what the bridge reports, and the overlay it is asked for
  let nodes = $state.raw<LayoutNode[]>([]);
  let hover = $state<string | null>(null);
  let editCapable = $state(false);
  let canvas = $state<{ select(path: string | null, edit: boolean): void; inlineEdit(path: string, value: string | null): void }>();
  const docFields = $derived(((doc as { fields?: Record<string, unknown> } | null)?.fields ?? {}) as Record<string, unknown>);
  const texts = $derived(canvasOn && doc ? textRefs(fields, docFields, (n) => blocksCatalog.get(n)) : []);
  $effect(() => {
    void blocksCatalog.load().catch(() => {});
  });
  // Text changes follow what the frame shows: a live frame gets them when it reports the patch rendered, a reload
  // frame asks for them with the `ready` after its reload. Turning the canvas on or off goes out at once.
  let lastOn: boolean | undefined;
  $effect(() => {
    const h = host;
    const on = canvasOn;
    const tx = texts;
    if (!h || on === lastOn) return;
    lastOn = on;
    h.setOverlay(on, tx);
  });
  $effect(() => {
    if (!canvasOn) {
      nodes = [];
      hover = null;
    }
  });
  const unsupported = $derived((status.phase === 'live' || status.phase === 'reload') && !editCapable);
  const canvasShown = $derived(canvasOn && editCapable && (status.phase === 'live' || status.phase === 'reload') && !!host);
  $effect(() => {
    void nonce;
    if (!origin) return;
    const h = new PreviewHost({
      origin,
      locked,
      token,
      frame: () => frame?.contentWindow ?? null,
      item: itemId,
      itemType: typeName,
      locale: lang,
      save: () => flush(),
      saveDelay: 50,
      onStatus: (s) => (status = s),
      onRendered: (n) => {
        ms = n;
        if (h.mode === 'live') h.setOverlay(untrack(() => canvasOn), untrack(() => texts));
      },
      onLayout: (n) => (nodes = n),
      onHover: (p) => (hover = p),
      onSelect: (p, edit) => canvas?.select(p, edit),
      onInlineEdit: (p, v) => canvas?.inlineEdit(p, v),
      onCapabilities: (e) => (editCapable = e),
      overlay: () => ({ on: untrack(() => canvasOn), texts: untrack(() => texts) }),
    });
    host = h;
    nodes = [];
    hover = null;
    editCapable = false;
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
  $effect(() => {
    const p = status.phase;
    onmode?.(p === 'live' || p === 'reload' ? p : null);
  });

  // ---- sharing
  let shareOpen = $state(false);
  const canShare = $derived(!!shares && session.can('content.preview.share', { type: typeName }));

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
            : status.phase === 'unverified'
              ? { tone: 'warning' as const, text: t('preview.status.unverified') }
          : { tone: 'neutral' as const, text: t('preview.status.connecting') },
  );
</script>

<div class="h-preview" data-preview-phase={status.phase}>
  <div class="h-preview__bar">
    {#if channels.length > 1}
      <Select compact aria-label={t('preview.channel')} value={channel?.name ?? ''} options={channels.map((c) => ({ value: c.name, label: c.name }))} onchange={(e) => (chosen = (e.currentTarget as HTMLSelectElement).value)} />
    {/if}
    <Segmented label={t('preview.device')} size="sm" options={devices} value={device} onchange={(v) => (device = v as Device)} />
    <IconButton
      icon="edit"
      size="sm"
      variant={canvasOn && !unsupported ? 'secondary' : 'ghost'}
      label={t('canvas.toggle')}
      aria-pressed={canvasOn && !unsupported}
      disabled={unsupported}
      title={unsupported ? t('canvas.unsupported') : t('canvas.toggle')}
      onclick={() => (canvasOn = !canvasOn)}
    />
    <span class="h-spacer"></span>
    {#if canShare}
      <Button size="sm" icon="link" title={t('preview.shareHint')} disabled={!channel} onclick={() => (shareOpen = true)}>{t('preview.share')}</Button>
    {/if}
    <IconButton icon="refresh" size="sm" label={t('preview.reload')} onclick={reload} disabled={phase !== 'ready'} />
    {#if src}
      <Button size="sm" icon="external-link" href={src} {...{ target: "_blank", rel: "noopener", "aria-label": t('preview.open'), title: t('preview.open') }} />
    {/if}
  </div>
  <div class="h-preview__status" role="status" aria-live="polite">
    {#if phase === 'ready'}
      <span title={status.phase === 'unverified' ? t('preview.unverifiedHint') : locked ? t('preview.lockedHint') : undefined}><Badge tone={chip.tone} dot>{chip.text}</Badge></span>
      {#if status.phase === 'unverified'}<span class="h-muted">{t('preview.unverifiedHint')}</span>{/if}
      {#if ms !== null}<span class="h-preview__ms h-num" title={t('preview.latencyLabel', { ms })}>{t('preview.latency', { ms })}</span>{/if}
      {#if status.phase === 'reload'}
        <span class="h-muted h-preview__save" class:h-preview__msg={relayState === 'invalid' || relayState === 'error' || relayState === 'stopped'} title={t('preview.relayHint')}>
          {#if relayState === 'sending'}{t('preview.relaySending')}{:else if relayState === 'invalid'}{t('preview.relayInvalid')}{:else if relayState === 'stopped'}{t('preview.relayStopped')}{:else if relayState === 'error'}{t('preview.relayError')}{:else if dirty}{t('preview.relayShown')}{/if}
        </span>
      {/if}
      {#if status.phase === 'error' && status.message}<span class="h-preview__msg">{status.message}</span>{/if}
    {/if}
  </div>
  {#if status.phase === 'reload' && !noticed}
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
        <div class="h-preview__deviceframe" tabindex="-1" style:width="{width * scale}px" style:height="{box.h}px">
          <iframe
            bind:this={frame}
            class="h-preview__frame"
            title={t('preview.frame', { title })}
            {src}
            sandbox={locked ? 'allow-scripts allow-forms allow-popups' : 'allow-scripts allow-same-origin allow-forms allow-popups'}
            referrerpolicy="no-referrer"
            style:width="{width}px"
            style:height="{scale ? box.h / scale : box.h}px"
            style:transform="scale({scale})"
            onload={() => host?.frameLoaded()}
          ></iframe>
          {#if canvasShown && host}
            <Canvas bind:this={canvas} {host} {fields} {docFields} {setField} {readonly} {scale} {nodes} {hover} />
          {/if}
        </div>
      {/key}
    {/if}
  </div>
</div>

{#if shares && canShare}
  <ShareDialog bind:open={shareOpen} item={itemId} channel={channel?.name ?? ''} locale={chLocale} {shares} />
{/if}

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
    position: relative;
    margin-inline: auto;
    overflow: hidden;
    background: #fff;
  }
  .h-preview__deviceframe:focus {
    outline: none;
  }
  .h-preview__frame {
    display: block;
    border: 0;
    background: #fff;
    transform-origin: 0 0;
  }
</style>
