// Binds a Form to the item's Yjs session. Imported lazily by the editor: this is the collab chunk with yjs.
import { tick } from 'svelte';
import { blocksCatalog } from '../blocks/catalog.svelte';
import { richCatalog } from '../richtext/custom.svelte';
import type { CompiledType } from '../types.svelte';
import type { Form } from '../form.svelte';
import { Binder } from './binder';
import type { CollabTransport, JoinResult } from './transport';
import { diffText, type Env } from './ydoc';

export { COLLAB_CTX } from './context';

export interface Binding {
  /** Local changes not yet acknowledged by the server (or not yet sent). */
  unsynced(): boolean;
  /** `discard`: the session is being restarted from the draft; unsent updates no longer fit it. */
  destroy(opts?: { discard?: boolean }): void;
  /** The highest update seq this client applied, its own acknowledged ones included (the save's `X-Hydrogen-Collab-Seq`). */
  applied(): number;
  /** Parts this client was seeding when its claim was lost (for the restart). */
  lostParts(): string[];
  /** What the server holds of the ready parts, for the restart's `resume`. */
  ackedValues(): { parts: string[]; base: Record<string, unknown> };
  /** Why local changes can no longer be sent ('' while all is well). */
  failed(): string;
  /** The Y.XmlFragment of a rich text field (`body[2].props.heading`), or undefined when it has none (yet). */
  fragment(fieldPath: string): unknown;
  /** Called after remote changes were written into the form. */
  onRemote(f: () => void): () => void;
}

/** Additions the editor can use: parts still being seeded by someone else, and when the first state is in. */
export interface BindingInfo extends Binding {
  pending(): string[];
  readonly ready: Promise<void>;
  /** True for a viewer, and once the server refused this client's updates. */
  readonly readOnly: boolean;
}

export function bindForm(o: {
  form: Form<any>;
  transport: CollabTransport;
  join: JoinResult;
  type: CompiledType;
  locale: string;
  draft: unknown;
  onFatal?(reason: 'claim_lost' | 'poison' | 'reset-failed'): void;
  /** The form holds only the overridden translatable fields (an inheriting locale). */
  sparse?: boolean;
  merge?: string[];
  resume?: { parts: string[]; base: Record<string, unknown> };
}): BindingInfo {
  let version = $state(0);
  const bump = () => version++;
  const listeners = new Set<() => void>();
  let caret: { el: HTMLInputElement | HTMLTextAreaElement; start: number; end: number } | undefined;

  const env: Env = { blockProps: (n) => blocksCatalog.get(n)?.props, sparse: o.sparse };
  const binder = new Binder({
    transport: o.transport,
    join: o.join,
    type: o.type,
    locale: o.locale,
    draft: o.draft,
    fields: () => o.form.values.fields as Record<string, unknown>,
    snapshot: (v) => $state.snapshot(v),
    env,
    schedule: (f) => (typeof requestAnimationFrame === 'function' ? requestAnimationFrame(() => f()) : queueMicrotask(f)),
    before: Promise.all([blocksCatalog.load(), richCatalog.load()]),
    onState: bump,
    onFatal: o.onFatal,
    merge: o.merge,
    resume: o.resume,
    hooks: {
      text(path, old, next) {
        const el = document.activeElement;
        if (!(el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement)) return;
        if (el.closest('[data-field]')?.getAttribute('data-field') !== path) return;
        const [at, del, ins] = diffText(old, next);
        const map = (p: number) => (p <= at ? p : p >= at + del ? p + ins.length - del : at + ins.length);
        caret = { el, start: map(el.selectionStart ?? 0), end: map(el.selectionEnd ?? 0) };
      },
    },
    afterProject() {
      const c = caret;
      caret = undefined;
      if (c) void tick().then(() => c.el.isConnected && c.el.setSelectionRange(c.start, c.end));
      for (const f of [...listeners]) f();
    },
  });

  const stop = $effect.root(() => {
    $effect(() => {
      void $state.snapshot(o.form.values.fields);
      binder.touch();
    });
  });

  return {
    unsynced() {
      void version;
      return binder.unsynced();
    },
    pending() {
      void version;
      return binder.pending();
    },
    fragment(path) {
      void version;
      return binder.fragment(path);
    },
    onRemote(f) {
      listeners.add(f);
      return () => void listeners.delete(f);
    },
    ready: binder.started,
    get readOnly() {
      void version;
      return binder.readOnly;
    },
    applied() {
      void version;
      return binder.applied();
    },
    lostParts: () => binder.lostParts(),
    ackedValues: () => binder.ackedValues(),
    failed() {
      void version;
      return binder.failed();
    },
    destroy(opts) {
      stop();
      binder.destroy(opts);
    },
  };
}
