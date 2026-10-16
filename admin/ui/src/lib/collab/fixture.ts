// A small schema and a fake server for the binding tests (not imported by the app).
import type { CompiledField } from '../types.svelte';
import type { CollabTransport, JoinResult, Participant, RemoteUpdate } from './transport';
import type { Env } from './ydoc';

const f = (name: string, base: string, extra: Partial<CompiledField> & { format?: string; enum?: string[] } = {}): CompiledField => {
  const { format, enum: en, ...rest } = extra;
  return { name, label: name, type: base, shape: { base, ...(format ? { format } : {}), ...(en ? { enum: en } : {}) }, ...rest };
};

export const sectionField = f('sections', 'list', {
  of: f('item', 'group', { fields: [f('heading', 'string'), f('body', 'richtext'), f('n', 'number')] }),
});

export const fields: CompiledField[] = [
  f('title', 'string', { translatable: true }),
  f('slug', 'string', { format: 'slug' }),
  f('count', 'number'),
  f('flag', 'boolean'),
  f('kind', 'string', { format: 'enum', enum: ['a', 'b', 'c'] }),
  f('tags', 'list', { of: f('tag', 'string') }),
  f('meta', 'group', { fields: [f('note', 'string', { format: 'text' }), f('level', 'number')] }),
  sectionField,
  f('body', 'richtext', { translatable: true }),
  f('blocks', 'blocks', { translatable: true }),
];

export const type = { name: 'page', fields } as { name: string; fields: CompiledField[] };

export const blockProps: Record<string, CompiledField[]> = {
  text: [
    f('heading', 'string'),
    f('body', 'richtext'),
    f('size', 'string', { format: 'enum', enum: ['s', 'm', 'l'] }),
    // A field with its own, narrower schema.
    f('quote', 'richtext', { options: { marks: ['bold'], nodes: ['paragraph'] } }),
  ],
  columns: [f('gap', 'number')],
};
export const slotNames: Record<string, string[]> = { text: [], columns: ['left', 'right'] };
export const env: Env = { blockProps: (t) => blockProps[t] };

export const para = (text: string) => ({ type: 'paragraph', content: text ? [{ type: 'text', text }] : undefined });
export const doc = (...texts: string[]) => ({ type: 'doc', content: texts.map(para) });

export const draft = {
  id: 'item1',
  locale: 'en',
  fields: {
    title: 'Hello',
    slug: 'hello',
    count: 1,
    meta: { note: 'n' },
    tags: ['x'],
    sections: [{ heading: 'one', n: 1 }, { heading: 'two', body: doc('two'), n: 2 }],
    body: doc('hello world'),
    blocks: [{ id: 'b0', type: 'text', props: { heading: 'H', body: doc('B') } }],
  } as Record<string, unknown>,
};

// ---- fake server

export interface Msg {
  u: RemoteUpdate;
}

export class FakeServer {
  log: RemoteUpdate[] = [];
  seq = 0;
  clients: FakeTransport[] = [];
  seeded = new Set<string>();
  n = 0;
  /**
   * Like the real server: a part is claimed by one client until it posts the seed (`init`), a claim lapses after
   * 15 s (`now` is the test's clock), others see the part as pending, and a seed without a claim is refused.
   */
  strict = false;
  now = 0;
  claims = new Map<string, { client: string; at: number }>();
  /** Strict mode: makes a post fail with a transient error. */
  failPost: ((client: string) => boolean) | null = null;

  connect(edit = true, client = ''): FakeTransport {
    const t = new FakeTransport(this, client || `c${++this.n}`, edit);
    this.clients.push(t);
    return t;
  }
}

export class FakeTransport implements Pick<CollabTransport, 'client' | 'join' | 'send' | 'compact' | 'onUpdate' | 'onCompact' | 'rejoin' | 'onRejoin' | 'onPresence'> {
  queue: RemoteUpdate[] = [];
  listeners = new Set<(u: RemoteUpdate) => void>();
  compactions: number[] = [];
  compactListeners = new Set<(n: number) => void>();
  rejoinListeners = new Set<(r: JoinResult) => void>();
  presenceListeners = new Set<(c: Participant[]) => void>();
  rejoins = 0;
  /** Parts the next rejoin hands to this client (the claim was released). */
  claim: string[] = [];
  failRejoin = false;
  sent = 0;
  failNext = 0;
  locale = 'en';
  constructor(
    readonly server: FakeServer,
    readonly client: string,
    readonly edit: boolean,
  ) {}

  async join(locale: string): Promise<JoinResult> {
    const s = this.server;
    this.locale = locale;
    const parts = ['shared', `l:${locale}`];
    if (s.strict) {
      const init: string[] = [];
      const pending: string[] = [];
      for (const p of parts) {
        if (s.seeded.has(p)) continue;
        const c = s.claims.get(p);
        if (!c || c.client === this.client || s.now - c.at >= 15_000) {
          // Joining again does not extend a claim.
          if (!c || c.client !== this.client || s.now - c.at >= 15_000) s.claims.set(p, { client: this.client, at: s.now });
          init.push(p);
        } else pending.push(p);
      }
      const me: Participant = { client: this.client, principal: 'p', name: 'n', color: '#000', locale, edit: this.edit };
      return { client: this.client, session: 's', seq: s.seq, updates: [...s.log], init, pending, presence: [me], etag: 'e' };
    }
    const init = parts.filter((p) => !s.seeded.has(p));
    for (const p of init) s.seeded.add(p);
    const me: Participant = { client: this.client, principal: 'p', name: 'n', color: '#000', locale, edit: this.edit };
    return { client: this.client, session: 's', seq: s.seq, updates: [...s.log], init, pending: [], presence: [me], etag: 'e' };
  }
  async send(data: Uint8Array, init?: string[]): Promise<number> {
    if (this.failNext > 0) {
      this.failNext--;
      throw new Error('offline');
    }
    const s = this.server;
    if (s.strict) {
      if (s.failPost?.(this.client)) throw Object.assign(new Error('unavailable'), { status: 503, code: 'unavailable' });
      for (const p of init ?? []) {
        const c = s.claims.get(p);
        if (s.seeded.has(p) || !c || c.client !== this.client || s.now - c.at >= 15_000) throw Object.assign(new Error('claim lost'), { status: 409, code: 'claim_lost' });
      }
      for (const p of init ?? []) s.seeded.add(p);
    }
    this.sent++;
    const u: RemoteUpdate = { seq: ++this.server.seq, data, client: this.client, ...(init ? { init } : {}) };
    this.server.log.push(u);
    for (const c of this.server.clients) if (c !== this) c.queue.push(u);
    return u.seq;
  }
  async compact(_state: Uint8Array, upto: number) {
    this.compactions.push(upto);
  }
  onUpdate(f: (u: RemoteUpdate) => void) {
    this.listeners.add(f);
    return () => void this.listeners.delete(f);
  }
  onCompact(f: (n: number) => void) {
    this.compactListeners.add(f);
    return () => void this.compactListeners.delete(f);
  }
  onRejoin(f: (r: JoinResult) => void) {
    this.rejoinListeners.add(f);
    return () => void this.rejoinListeners.delete(f);
  }
  onPresence(f: (c: Participant[]) => void) {
    this.presenceListeners.add(f);
    return () => void this.presenceListeners.delete(f);
  }
  /** Like the real transport: the log replays through onUpdate, then the answer goes to onRejoin. */
  async rejoin(): Promise<void> {
    this.rejoins++;
    if (this.failRejoin) throw new Error('down');
    const r = await this.join(this.locale);
    const init = this.server.strict ? r.init : this.claim.splice(0);
    if (!this.server.strict) for (const p of init) this.server.seeded.add(p);
    const res: JoinResult = { ...r, init };
    for (const u of res.updates) for (const f of [...this.listeners]) f(u);
    for (const f of [...this.rejoinListeners]) f(res);
  }
  deliver(i: number, keep = false) {
    const u = keep ? this.queue[i]! : this.queue.splice(i, 1)[0]!;
    for (const f of [...this.listeners]) f(u);
  }
}
