import { flushSync } from 'svelte';
import * as Y from 'yjs';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { blocksCatalog } from '../blocks/catalog.svelte';
import { Form } from '../form.svelte';
import { hydrate } from '../fields/logic';
import { Binder } from './binder';
import { bindForm } from './bind.svelte';
import { doc, draft, env, FakeServer, type, blockProps } from './fixture';
import type { CollabTransport, JoinResult } from './transport';
import { jsonEq, toPlain, type Doc } from './ydoc';

beforeAll(() => {
  blocksCatalog.blocks = Object.entries(blockProps).map(([name, props]) => ({ name, label: name, props, slots: [] }));
  blocksCatalog.loaded = true;
});

const values = () => ({ fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc, key: '', alwaysAvailable: false });
const settle = () => new Promise<void>((r) => setTimeout(r, 40));

async function open(server: FakeServer) {
  const t = server.connect();
  const join = await t.join('en');
  const form = new Form(values());
  const b = bindForm({ form, transport: t as unknown as CollabTransport, join: join as JoinResult, type: type as never, locale: 'en', draft });
  await b.ready;
  return { t, form, b, join };
}

describe('bindForm', () => {
  it('syncs edits between two forms and tracks acknowledgements', async () => {
    const s = new FakeServer();
    const a = await open(s);
    const b = await open(s);
    await settle();
    expect(a.b.unsynced()).toBe(false);
    (a.form.values.fields as Doc).title = 'Hello there';
    ((a.form.values.fields as Doc).blocks as Doc[]).push({ id: 'n1', type: 'text', props: { heading: 'new' } });
    flushSync();
    await vi.waitFor(() => expect(a.b.unsynced()).toBe(true));
    await settle();
    expect(a.b.unsynced()).toBe(false);
    for (const u of b.t.queue.splice(0)) for (const f of b.t.listeners) f(u);
    expect((b.form.values.fields as Doc).title).toBe('Hello there');
    expect(((b.form.values.fields as Doc).blocks as Doc[]).map((x) => x.id)).toEqual(['b0', 'n1']);
    expect(b.form.dirty).toBe(true);
    let remote = 0;
    b.b.onRemote(() => remote++);
    (a.form.values.fields as Doc).count = 7;
    flushSync();
    await settle();
    for (const u of b.t.queue.splice(0)) for (const f of b.t.listeners) f(u);
    expect((b.form.values.fields as Doc).count).toBe(7);
    expect(remote).toBeGreaterThan(0);
    a.b.destroy();
    b.b.destroy();
  });

  it('does not write a remote change into the field being edited elsewhere and keeps the caret', async () => {
    const s = new FakeServer();
    const a = await open(s);
    const b = await open(s);
    document.body.innerHTML = '<div data-field="title"><input id="t" value="Hello"></div>';
    const input = document.getElementById('t') as HTMLInputElement;
    input.focus();
    input.setSelectionRange(2, 2);
    // What the bound input does when the value changes: assigning it moves the caret to the end.
    b.b.onRemote(() => (input.value = String((b.form.values.fields as Doc).title)));
    const slugBefore = (b.form.values.fields as Doc).slug;
    (a.form.values.fields as Doc).title = 'Oh Hello';
    flushSync();
    await settle();
    for (const u of b.t.queue.splice(0)) for (const f of b.t.listeners) f(u);
    await settle();
    expect((b.form.values.fields as Doc).title).toBe('Oh Hello');
    expect((b.form.values.fields as Doc).slug).toBe(slugBefore);
    expect(input.selectionStart).toBe(5);
    a.b.destroy();
    b.b.destroy();
  });

  it('exposes fragments of rich text fields only for ready parts', async () => {
    const s = new FakeServer();
    const a = await open(s);
    expect(a.b.fragment('body')).toBeInstanceOf(Y.XmlFragment);
    expect(a.b.fragment('blocks[0].props.body')).toBeInstanceOf(Y.XmlFragment);
    expect(a.b.fragment('sections[1].body')).toBeInstanceOf(Y.XmlFragment);
    expect(a.b.fragment('title')).toBeUndefined();
    expect(a.b.fragment('nope')).toBeUndefined();
    a.b.destroy();
  });
});

describe('Binder', () => {
  function make(t: ReturnType<FakeServer['connect']>, join: JoinResult, v: { fields: Doc }, extra: Partial<ConstructorParameters<typeof Binder>[0]> = {}) {
    const ticks: (() => void)[] = [];
    const b = new Binder({ transport: t, join, type, locale: 'en', draft, fields: () => v.fields, snapshot: (x) => structuredClone(x), env, schedule: (f) => ticks.push(f), ...extra });
    return { b, ticks };
  }

  it('keeps edits made before the session was joined and takes the rest from the merged state', async () => {
    const s = new FakeServer();
    const t1 = s.connect();
    const j1 = await t1.join('en');
    const v1 = { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc };
    const c1 = make(t1, j1, v1);
    await c1.b.started;
    v1.fields.count = 42;
    v1.fields.title = 'Other';
    c1.b.flush();
    await settle();

    const t2 = s.connect();
    const j2 = await t2.join('en');
    const v2 = { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc };
    v2.fields.slug = 'mine';
    const c2 = make(t2, j2, v2);
    await c2.b.started;
    expect(v2.fields.count).toBe(42);
    expect(v2.fields.title).toBe('Other');
    expect(v2.fields.slug).toBe('mine');
    c2.b.flush();
    await settle();
    expect(toPlain(c2.b.doc.getMap('shared'))).toMatchObject({ slug: 'mine', count: 42 });
  });

  it('re-joins through the transport for pending parts and seeds the ones it is handed', async () => {
    vi.useFakeTimers();
    try {
      const s = new FakeServer();
      const t1 = s.connect();
      await t1.join('en'); // claims both parts, never seeds them
      const t2 = s.connect();
      const v2 = { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc };
      const j2: JoinResult = { client: t2.client, session: 's', seq: 0, updates: [], init: [], pending: ['shared', 'l:en'], presence: [], etag: 'e' };
      const c2 = make(t2, j2, v2);
      await vi.advanceTimersByTimeAsync(0);
      expect(c2.b.pending()).toEqual(['shared', 'l:en']);
      await vi.advanceTimersByTimeAsync(15_000);
      expect(t2.rejoins).toBe(0);
      t2.claim = ['shared', 'l:en']; // the seeder is gone: the claim is released
      await vi.advanceTimersByTimeAsync(1_100);
      expect(t2.rejoins).toBe(1);
      expect(c2.b.pending()).toEqual([]);
      expect(t2.sent).toBe(1); // seeded and posted once, no second session
      c2.b.destroy();
    } finally {
      vi.useRealTimers();
    }
  });

  it('re-joins on a presence event while parts are pending, but not in a loop', async () => {
    vi.useFakeTimers();
    try {
      const s = new FakeServer();
      const t2 = s.connect();
      const v2 = { fields: hydrate(type.fields, {}) as Doc };
      const j2: JoinResult = { client: t2.client, session: 's', seq: 0, updates: [], init: [], pending: ['shared', 'l:en'], presence: [], etag: 'e' };
      const c2 = make(t2, j2, v2);
      await vi.advanceTimersByTimeAsync(0);
      const poke = () => t2.presenceListeners.forEach((f) => f([]));
      poke();
      poke();
      await vi.advanceTimersByTimeAsync(0);
      expect(t2.rejoins).toBe(1);
      poke();
      expect(t2.rejoins).toBe(1); // cooling down
      await vi.advanceTimersByTimeAsync(3_100);
      poke();
      expect(t2.rejoins).toBe(2);
      c2.b.destroy();
    } finally {
      vi.useRealTimers();
    }
  });

  it('waits for a pending part and applies it when its init update arrives', async () => {
    const s = new FakeServer();
    const t1 = s.connect();
    const j1 = await t1.join('en');
    const v1 = { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc };
    const t2 = s.connect();
    const v2 = { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc };
    const j2: JoinResult = { ...j1, client: t2.client, init: [], pending: ['shared', 'l:en'], updates: [], presence: [] };
    const c2 = make(t2, j2, v2);
    await c2.b.started;
    expect(c2.b.pending()).toEqual(['shared', 'l:en']);
    expect(c2.b.fragment('body')).toBeUndefined();
    v2.fields.count = 5;
    c2.b.flush();
    expect(toPlain(c2.b.doc.getMap('shared'))).toEqual({});
    const c1 = make(t1, j1, v1);
    await c1.b.started;
    await settle();
    for (const u of t2.queue.splice(0)) for (const f of t2.listeners) f(u);
    expect(c2.b.pending()).toEqual([]);
    expect(v2.fields.title).toBe('Hello');
    // The edit made while the part was pending stays, on top of the seed.
    expect(v2.fields.count).toBe(5);
    expect(toPlain(c2.b.doc.getMap('shared'))).toMatchObject({ count: 5, slug: 'hello' });
    expect(c2.b.fragment('body')).toBeInstanceOf(Y.XmlFragment);
  });

  it('sends nothing for a viewer', async () => {
    const s = new FakeServer();
    const t1 = s.connect();
    const j1 = await t1.join('en');
    const c1 = make(t1, j1, { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc });
    await c1.b.started;
    await settle();
    const t2 = s.connect(false);
    const j2 = await t2.join('en');
    const v2 = { fields: hydrate(type.fields, {}) as Doc };
    const c2 = make(t2, j2, v2);
    await c2.b.started;
    expect(c2.b.readOnly).toBe(true);
    v2.fields.count = 99;
    c2.b.flush();
    expect(t2.sent).toBe(0);
    expect(v2.fields.title).toBe('Hello');
  });

  it('retries a failed send and reports unsynced until it is acknowledged', async () => {
    const s = new FakeServer();
    const t = s.connect();
    const j = await t.join('en');
    const v = { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc };
    const timers: (() => void)[] = [];
    const c = make(t, j, v, { setTimeout: (f) => (timers.push(f), 1) });
    await c.b.started;
    await settle();
    t.failNext = 1;
    v.fields.count = 3;
    c.b.flush();
    await settle();
    expect(c.b.unsynced()).toBe(true);
    expect(timers).toHaveLength(1);
    timers[0]!();
    await settle();
    expect(c.b.unsynced()).toBe(false);
    expect(s.log.at(-1)!.client).toBe(t.client);
  });

  it('compacts only when it has applied everything up to the request', async () => {
    const s = new FakeServer();
    const t = s.connect();
    const j = await t.join('en');
    const c = make(t, j, { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc });
    await c.b.started;
    await settle();
    const seq = s.seq;
    for (const f of t.compactListeners) f(seq + 10);
    expect(t.compactions).toEqual([]);
    for (const f of t.compactListeners) f(seq);
    await settle();
    expect(t.compactions).toEqual([seq]);
  });

  async function pair() {
    const s = new FakeServer();
    const mk = async () => {
      const t = s.connect();
      const v = { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc };
      const c = make(t, await t.join('en'), v);
      await c.b.started;
      return { t, v, ...c };
    };
    const a = await mk();
    const b = await mk();
    await settle();
    exchange([a, b]);
    return [a, b] as const;
  }
  const exchange = (cs: { t: ReturnType<FakeServer['connect']> }[]) => {
    for (const c of cs) for (const u of c.t.queue.splice(0)) for (const f of c.t.listeners) f(u);
  };

  // Items of a list of groups have no ids: two editors adding an empty item at once hold equal values, and the
  // remote one must not take over the form object (and so the input) the local editor is typing into.
  it.each([
    ['after both items were exchanged', false],
    ['while the own item is still unsent', true],
  ])('list of groups: an item added concurrently with a peer keeps what its editor types (%s)', async (_, unsent) => {
    const [a, b] = await pair();
    const mine = new Map<(typeof a) | (typeof b), Doc>();
    for (const c of [a, b]) {
      const item: Doc = { heading: '' };
      (c.v.fields.sections as Doc[]).splice(0, 0, item);
      mine.set(c, item);
      if (!unsent || c === b) c.b.flush();
    }
    await settle();
    // A receives B's item before its own went out (the receive flushes A's form first), or after.
    exchange([a, b]);
    a.b.flush();
    await settle();
    exchange([a, b]);
    for (const [c, text] of [[a, 'from a'], [b, 'from b']] as const) {
      expect((c.v.fields.sections as Doc[]).includes(mine.get(c)!), `${text}: own item still in the form`).toBe(true);
      mine.get(c)!.heading = text;
      c.b.flush();
    }
    await settle();
    exchange([a, b]);
    for (const c of [a, b]) {
      const heads = (c.v.fields.sections as Doc[]).map((x) => x.heading);
      expect([...heads].sort()).toEqual(['from a', 'from b', 'one', 'two']);
      expect(jsonEq(c.v.fields.sections, (toPlain(c.b.doc.getMap('shared')) as Doc).sections)).toBe(true);
    }
    expect(a.v.fields.sections).toEqual(b.v.fields.sections);
  });

  it('list of groups: removing one of two equal items removes that one, not the one a peer is typing into', async () => {
    const [a, b] = await pair();
    const add = async (c: typeof a) => {
      const item: Doc = { heading: '' };
      (c.v.fields.sections as Doc[]).push(item);
      c.b.flush();
      await settle();
      exchange([a, b]);
      return item;
    };
    const x = await add(a);
    const y = await add(b);
    const as = a.v.fields.sections as Doc[];
    as.splice(as.indexOf(x), 1);
    y.heading = 'from b';
    a.b.flush();
    b.b.flush();
    await settle();
    exchange([a, b]);
    for (const c of [a, b]) expect((c.v.fields.sections as Doc[]).map((i) => i.heading)).toEqual(['one', 'two', 'from b']);
  });

  it('a late joiner reaches the same state and gets duplicates removed', async () => {
    const s = new FakeServer();
    const mk = async () => {
      const t = s.connect();
      const j = await t.join('en');
      const v = { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc };
      const c = make(t, j, v);
      await c.b.started;
      return { t, v, ...c };
    };
    const a = await mk();
    const b = await mk();
    await settle();
    for (const u of b.t.queue.splice(0)) for (const f of b.t.listeners) f(u);
    for (const u of a.t.queue.splice(0)) for (const f of a.t.listeners) f(u);
    for (const c of [a, b]) {
      const blocks = c.v.fields.blocks as Doc[];
      blocks.push({ id: 'x', type: 'columns', props: {}, slots: { left: [] } });
      blocks.push({ id: 'y', type: 'text', props: {} });
      c.b.flush();
    }
    await settle();
    for (const c of [a, b]) {
      const blocks = c.v.fields.blocks as Doc[];
      const [x] = blocks.splice(1, 1);
      blocks.push(x!);
      c.b.flush();
    }
    await settle();
    for (const c of [a, b]) for (const u of c.t.queue.splice(0)) for (const f of c.t.listeners) f(u);
    const late = await mk();
    await settle();
    const ids = (late.v.fields.blocks as Doc[]).map((x) => x.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(jsonEq(late.v.fields, toPlain(late.b.doc.getMap('l:en')) as Doc)).toBe(false); // l:en holds only translatable fields
    expect(jsonEq(late.v.fields.blocks, (toPlain(late.b.doc.getMap('l:en')) as Doc).blocks)).toBe(true);
  });
});

describe('bindForm with sparse documents (overlay locales)', () => {
  // An inheriting locale's form holds only the shared fields and the overrides.
  const sparse = (o: Doc = {}) => ({ fields: { slug: 'hello', count: 1, ...o } as Doc, key: '', alwaysAvailable: false });
  const sparseDraft = { ...draft, fields: { slug: 'hello', count: 1, title: 'Hallo' } as Record<string, unknown> };

  async function openSparse(server: FakeServer, v = sparse({ title: 'Hallo' })) {
    const t = server.connect();
    const join = await t.join('en');
    const form = new Form(v);
    const b = bindForm({ form, transport: t as unknown as CollabTransport, join: join as JoinResult, type: type as never, locale: 'en', draft: sparseDraft, sparse: true });
    await b.ready;
    return { t, form, b };
  }
  const pump = (x: { t: { queue: unknown[]; listeners: Set<(u: never) => void> } }) => {
    for (const u of x.t.queue.splice(0)) for (const f of x.t.listeners) f(u as never);
  };

  it('syncs removing an override and adding it back', async () => {
    const s = new FakeServer();
    const a = await openSparse(s);
    const b = await openSparse(s);
    await settle();
    const fa = a.form.values.fields as Doc;
    const fb = b.form.values.fields as Doc;
    expect('body' in fb).toBe(false);

    delete fa.title;
    flushSync();
    await settle();
    pump(b);
    expect('title' in fb).toBe(false);

    fa.title = 'Servus';
    flushSync();
    await settle();
    pump(b);
    expect(fb.title).toBe('Servus');

    // An override of a field neither document had before: absent on both sides, then present.
    fa.body = doc('override');
    flushSync();
    await settle();
    pump(b);
    expect(fb.body).toEqual(doc('override'));
    delete fa.body;
    flushSync();
    await settle();
    pump(b);
    expect('body' in fb).toBe(false);
    expect(fb.slug).toBe('hello');

    // An empty override is an override, not a missing one.
    fa.title = '';
    flushSync();
    await settle();
    pump(b);
    expect(fb.title).toBe('');
    delete fa.title;
    flushSync();
    await settle();
    pump(b);
    expect('title' in fb).toBe(false);
    a.b.destroy();
    b.b.destroy();
  });

  it('a peer removing an override never removes the shared fields or other overrides', async () => {
    const s = new FakeServer();
    const a = await openSparse(s, sparse({ title: 'Hallo', body: doc('x') }));
    const b = await openSparse(s, sparse({ title: 'Hallo', body: doc('x') }));
    await settle();
    delete (b.form.values.fields as Doc).title;
    flushSync();
    await settle();
    pump(a);
    const fa = a.form.values.fields as Doc;
    expect('title' in fa).toBe(false);
    expect(fa.body).toEqual(doc('x'));
    expect(fa.count).toBe(1);
    a.b.destroy();
    b.b.destroy();
  });

  it('applies an override removed before the session was joined', async () => {
    const s = new FakeServer();
    const a = await openSparse(s);
    await settle();
    const t = s.connect();
    const join = await t.join('en');
    const form = new Form(sparse({ title: 'Hallo' }));
    delete (form.values.fields as Doc).title;
    const b = bindForm({ form, transport: t as unknown as CollabTransport, join: join as JoinResult, type: type as never, locale: 'en', draft: sparseDraft, sparse: true });
    await b.ready;
    flushSync();
    await settle();
    pump(a);
    expect('title' in (a.form.values.fields as Doc)).toBe(false);
    expect('title' in (form.values.fields as Doc)).toBe(false);
    a.b.destroy();
    b.destroy();
  });
});

describe('switching locales', () => {
  const en = () => structuredClone(draft.fields);
  const sharedOnly = () => {
    const f = en();
    for (const k of ['title', 'body', 'blocks']) delete f[k];
    return f;
  };
  const fresh = (f: Doc) => ({ fields: hydrate(type.fields, structuredClone(f)) as Doc, key: '', alwaysAvailable: false });
  async function visit(s: FakeServer, locale: string, fields: Doc, form = new Form(fresh(fields))) {
    const t = s.connect();
    const join = await t.join(locale);
    const b = bindForm({ form, transport: t as unknown as CollabTransport, join: join as JoinResult, type: type as never, locale, draft: { ...draft, locale, fields } });
    await b.ready;
    await settle();
    return { form, b };
  }

  // The editor stops the binding before it replaces the form: destroy flushes the form into the binding's parts.
  it('keeps the left locale intact when the binding stops before the form is replaced', async () => {
    const s = new FakeServer();
    const a = await visit(s, 'en', en());
    a.b.destroy();
    a.form.reset(fresh(sharedOnly()));
    const back = await visit(s, 'en', en());
    expect(back.form.dirty).toBe(false);
    expect((back.form.values.fields as Doc).title).toBe('Hello');
  });

  it('empties the left locale for the next visit when the form is replaced first', async () => {
    const s = new FakeServer();
    const a = await visit(s, 'en', en());
    a.form.reset(fresh(sharedOnly()));
    a.b.destroy();
    await settle();
    const back = await visit(s, 'en', en());
    expect(back.form.dirty).toBe(true);
  });
});
