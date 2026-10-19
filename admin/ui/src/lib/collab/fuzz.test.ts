import { prosemirrorJSONToYXmlFragment, ySyncPlugin } from 'y-prosemirror';
import { wrapInList } from 'prosemirror-schema-list';
import { EditorState } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import * as Y from 'yjs';
import { describe, expect, it } from 'vitest';
import { hydrate } from '../fields/logic';
import { isEmptyDoc, toStored } from '../richtext/schema';
import { Binder } from './binder';
import { draft, env, FakeServer, FakeTransport, slotNames, type } from './fixture';
import { jsonEq, richSchema, toPlain, type Doc } from './ydoc';

// What it covers: convergence of 3-5 clients over a server that claims parts for seeding (async claims, expiry,
// claim_lost and restarts), transient post failures with retries, re-joins that replay the log, duplicated and
// reordered delivery, late joiners with edits made before they joined, two locales, and rich text edited through
// ProseMirror and y-prosemirror in a quarter of the runs. Intent oracle: unique single-character tokens inserted
// into a text field, blocks marked `keep-`, and tokens typed into the rich text (runs without structural edits)
// must survive exactly once in every client of the locale. Lists of groups: items an editor adds (empty, so equal
// to the ones others add at the same time) and then types tokens into, by the form object it added, keep those
// tokens exactly once, and tokens typed into different items never end up in one.
const RUNS = Number(process.env.COLLAB_FUZZ_RUNS ?? 1500);

function rng(seed: number) {
  let a = seed >>> 0;
  const next = () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const int = (n: number) => Math.floor(next() * n);
  const pick = <T>(a: readonly T[]) => a[int(a.length)]!;
  return { next, int, pick, chance: (p: number) => next() < p };
}
type Rng = ReturnType<typeof rng>;

const ALPHA = ['a', 'b', 'c', ' ', 'x', 'é', '😀', 'z'];
const word = (r: Rng) => Array.from({ length: 1 + r.int(4) }, () => r.pick(ALPHA)).join('');

function splice(r: Rng, s: string) {
  const chars = [...s];
  const at = r.int(chars.length + 1);
  const del = r.chance(0.4) ? r.int(Math.min(3, chars.length - at) + 1) : 0;
  chars.splice(at, del, ...(r.chance(0.8) ? [...word(r)] : []));
  return chars.join('');
}


// ---- clock (the binder's retry timers and the server's claim expiry run on it)

class Clock {
  now = 0;
  nid = 0;
  timers: { at: number; fn: () => void; id: number }[] = [];
  constructor(readonly server: FakeServer) {}
  setTimeout = (fn: () => void, ms: number) => {
    const id = ++this.nid;
    this.timers.push({ at: this.now + ms, fn, id });
    return id;
  };
  clearTimeout = (h: unknown) => {
    this.timers = this.timers.filter((t) => t.id !== h);
  };
  advance(ms: number) {
    const end = this.now + ms;
    for (;;) {
      const due = this.timers.filter((t) => t.at <= end).sort((a, b) => a.at - b.at || a.id - b.id)[0];
      if (!due) break;
      this.timers = this.timers.filter((t) => t !== due);
      this.now = due.at;
      this.server.now = this.now;
      due.fn();
    }
    this.now = end;
    this.server.now = end;
  }
}

interface Client {
  id: number;
  locale: string;
  t: FakeTransport;
  b: Binder | undefined;
  v: { fields: Doc };
  tick: (() => void)[];
  fatal: string;
  merge?: string[];
  resume?: { parts: string[]; base: Doc };
  pm?: { view: EditorView; frag: Y.XmlFragment };
  /** List items this client added and types into (`n: -1`; the other list edits leave them alone). */
  own: { item: Doc; key: number; safe: boolean }[];
  typed: string[];
  /** Rich text tokens typed while this client was still seeding the part (its claim may yet be lost). */
  seedTyped: string[];
}

interface Oracle {
  note: Set<string>;
  keep: Map<string, Set<string>>;
  body: Map<string, Set<string>>;
  /** Rich text tokens a restart after a lost claim may drop (the part is merged as a whole then): at most once. */
  bodyRisk: Set<string>;
  /** Token typed into an own list item -> that item's key. */
  sect: Map<string, number>;
  /** Tokens typed where they cannot be lost (a ready part, before any restart of the typing client). */
  sectSafe: Set<string>;
}

let ids = 0;
let dups = 0;
let restarts = 0;
let claimLosts = 0;
let pmEdits = 0;
const newId = (c: number) => `blk-${c}-${ids++}`;

// One private-use character per token: an insert is a single character, so concurrent typing cannot split it.
let tokens = 0;
const token = () => String.fromCodePoint(0xe000 + (tokens++ % 6000));

interface Lists {
  list: Doc[];
  owner?: Doc;
}
function lists(root: Doc[]): Lists[] {
  const out: Lists[] = [{ list: root }];
  const walk = (l: Doc[]) => {
    for (const b of l) {
      const slots = b.slots as Record<string, Doc[]> | undefined;
      for (const k of Object.keys(slots ?? {})) {
        out.push({ list: slots![k]!, owner: b });
        walk(slots![k]!);
      }
    }
  };
  walk(root);
  return out;
}
const hasId = (b: Doc, id: unknown): boolean => b.id === id || Object.values((b.slots ?? {}) as Record<string, Doc[]>).some((l) => l.some((x) => hasId(x, id)));


function newBlock(r: Rng, c: number): Doc {
  const t = r.pick(['text', 'text', 'columns']);
  const b: Doc = { id: newId(c), type: t, props: t === 'text' ? { heading: word(r) } : {} };
  if (t === 'columns') b.slots = r.chance(0.5) ? { left: [], right: [] } : { left: [] };
  return b;
}

function allBlocks(root: Doc[]): Doc[] {
  return lists(root).flatMap((l) => l.list);
}


const isKeep = (b: Doc) => typeof b.id === 'string' && b.id.startsWith('keep-');

function richFragmentsOf(c: Client): Y.XmlFragment[] {
  const out: Y.XmlFragment[] = [];
  const f = c.b!.fragment('body');
  if (f) out.push(f as Y.XmlFragment);
  const fields = c.v.fields;
  const walk = (l: Doc[], path: string) =>
    l.forEach((b, i) => {
      const p = `${path}[${i}]`;
      const fr = c.b!.fragment(`${p}.props.body`);
      if (fr) out.push(fr as Y.XmlFragment);
      for (const [k, sl] of Object.entries((b.slots ?? {}) as Record<string, Doc[]>)) walk(sl, `${p}.slots.${k}`);
    });
  walk((fields.blocks ?? []) as Doc[], 'blocks');
  ((fields.sections ?? []) as Doc[]).forEach((_, i) => {
    const fr = c.b!.fragment(`sections[${i}].body`);
    if (fr) out.push(fr as Y.XmlFragment);
  });
  return out;
}

function editRich(r: Rng, c: Client) {
  const frs = richFragmentsOf(c);
  if (!frs.length) return;
  const frag = r.pick(frs);
  c.b!.doc.transact(() => {
    const paras = frag.toArray();
    const texts = paras.filter((p): p is Y.XmlElement => p instanceof Y.XmlElement);
    const roll = r.next();
    if (roll < 0.5 && texts.length) {
      const el = r.pick(texts);
      const tx = el.toArray().find((x) => x instanceof Y.XmlText) as Y.XmlText | undefined;
      if (tx) {
        const at = r.int(tx.length + 1);
        if (r.chance(0.3) && tx.length > at) tx.delete(at, 1);
        else tx.insert(at, r.pick(['a', 'b', 'é']));
      } else el.insert(0, [new Y.XmlText(word(r))]);
    } else if (roll < 0.8) {
      const json = { type: 'paragraph', content: [{ type: 'text', text: word(r) }] };
      const tmp = new Y.XmlFragment();
      const d = new Y.Doc();
      d.getMap('x').set('f', tmp);
      prosemirrorJSONToYXmlFragment(richSchema(), { type: 'doc', content: [json] }, tmp);
      frag.insert(r.int(paras.length + 1), [tmp.get(0).clone() as Y.XmlElement]);
    } else if (paras.length > 1) {
      frag.delete(r.int(paras.length), 1);
    }
  }, 'editor');
}

// ---- rich text through ProseMirror + y-prosemirror (the editor's path)

const bodyDef = type.fields.find((f) => f.name === 'body')!;

function pmView(c: Client): EditorView | undefined {
  const frag = c.b?.fragment('body') as Y.XmlFragment | undefined;
  if (!frag) return undefined;
  if (c.pm && c.pm.frag === frag) return c.pm.view;
  c.pm?.view.destroy();
  const host = document.createElement('div');
  const view = new EditorView(host, { state: EditorState.create({ schema: richSchema(bodyDef), plugins: [ySyncPlugin(frag)] }) });
  c.pm = { view, frag };
  return view;
}

/** Edits the `body` field the way the editor does: through a transaction; the form follows like Editor.svelte's emit. */
function editPM(r: Rng, c: Client, structural: boolean, or: Oracle) {
  const view = pmView(c);
  if (!view) return false;
  const st = view.state;
  const blocks: { pos: number; size: number }[] = [];
  st.doc.descendants((n, pos) => {
    if (n.isTextblock) blocks.push({ pos: pos + 1, size: n.content.size });
    return true;
  });
  if (!blocks.length) return false;
  const tb = r.pick(blocks);
  const at = tb.pos + r.int(tb.size + 1);
  const roll = r.next();
  const sc = st.schema;
  try {
    if (roll < 0.55 || !structural && roll < 0.8) {
      const tok = token();
      view.dispatch(st.tr.insertText(tok, at));
      const set = or.body.get(c.locale) ?? new Set<string>();
      set.add(tok);
      or.body.set(c.locale, set);
      if (c.b!.lostParts().includes(`l:${c.locale}`)) c.seedTyped.push(tok);
    } else if (roll < 0.8 || !structural) {
      if (tb.size > 1 && sc.marks.bold) view.dispatch(st.tr.addMark(tb.pos, tb.pos + tb.size, sc.marks.bold.create()));
    } else if (roll < 0.9) view.dispatch(st.tr.split(at));
    else if (roll < 0.95 && sc.nodes.heading) view.dispatch(st.tr.setBlockType(at, at, sc.nodes.heading, { level: 2 }));
    else if (sc.nodes.bulletList) {
      view.dom.focus?.();
      view.dispatch(st.tr.setSelection(st.selection.constructor === st.selection.constructor ? st.selection : st.selection));
      wrapInList(sc.nodes.bulletList)(view.state, view.dispatch);
    }
  } catch {
    return false;
  }
  pmEdits++;
  const doc = view.state.doc;
  c.v.fields.body = isEmptyDoc(doc.toJSON()) ? undefined : toStored(doc);
  return true;
}

let owns = 0;
const isOwn = (x: Doc) => x.n === -1;
const inSession = (c: Client) => !!c.b?.isReady('shared') && !c.b.lostParts().includes('shared');

function typeInto(r: Rng, c: Client, or: Oracle, o: Client['own'][number]) {
  const chars = [...((o.item.heading as string) ?? '')];
  const tok = token();
  chars.splice(r.int(chars.length + 1), 0, tok);
  o.item.heading = chars.join('');
  or.sect.set(tok, o.key);
  c.typed.push(tok);
  if (inSession(c)) or.sectSafe.add(tok);
}

function addOwn(r: Rng, c: Client, or: Oracle, typeNow: boolean) {
  const s = ((c.v.fields.sections ??= []) as Doc[]);
  const o = { item: { heading: '', n: -1 } as Doc, key: owns++, safe: inSession(c) };
  s.splice(r.chance(0.7) ? s.length : r.int(s.length + 1), 0, o.item);
  c.own.push(o);
  if (typeNow) typeInto(r, c, or, o);
}

function edit(r: Rng, c: Client, or: Oracle, pmMode: '' | 'plain' | 'struct') {
  const f = c.v.fields;
  const roll = r.int(17);
  switch (roll) {
    case 0:
      f.title = splice(r, (f.title as string) ?? '');
      break;
    case 1:
      f.slug = splice(r, (f.slug as string) ?? '');
      break;
    case 2:
      f.count = r.int(100);
      break;
    case 3:
      f.flag = r.chance(0.5);
      break;
    case 4:
      f.kind = r.chance(0.2) ? undefined : r.pick(['a', 'b', 'c']);
      break;
    case 5:
      f.tags = Array.from({ length: r.int(4) }, () => word(r));
      break;
    case 6: {
      const meta = ((f.meta ??= {}) as Doc);
      if (r.chance(0.7)) {
        // Insert-only text (the intent oracle): one new character at a random place.
        const chars = [...((meta.note as string) ?? '')];
        const tok = token();
        chars.splice(r.int(chars.length + 1), 0, tok);
        meta.note = chars.join('');
        or.note.add(tok);
      } else meta.level = r.int(10);
      break;
    }
    case 7: {
      const s = ((f.sections ??= []) as Doc[]);
      const k = r.int(7);
      const others = s.map((x, i) => [x, i] as const).filter(([x]) => !isOwn(x));
      // Not items added before the join: the join's merge may have given their form objects to other items.
      const mine = c.own.filter((o) => o.safe && s.includes(o.item));
      if (k >= 4) {
        // Like the list widget: an empty item, typed into right away or later, by the object the editor added.
        if (k === 4 || !mine.length) addOwn(r, c, or, r.chance(0.3));
        else typeInto(r, c, or, r.pick(mine));
      } else if (k === 0 || !others.length) s.splice(r.int(s.length + 1), 0, { heading: word(r), n: r.int(9) });
      else if (k === 1) s.splice(r.pick(others)[1], 1);
      else if (k === 2) {
        const [it] = r.pick(others);
        if (r.chance(0.7)) it.heading = splice(r, (it.heading as string) ?? '');
        else it.n = r.int(9);
      } else if (others.length > 1) {
        const [x] = s.splice(r.pick(others)[1], 1);
        s.splice(r.int(s.length + 1), 0, x!);
      }
      break;
    }
    case 8:
    case 9:
    case 10: {
      const root = ((f.blocks ??= []) as Doc[]);
      const ls = lists(root);
      const k = r.int(6);
      const plain = ls.map((l) => ({ ...l, list: l.list }));
      if (k === 5) {
        // A block that nobody removes or moves: the intent oracle checks it survives.
        const b: Doc = { id: `keep-${newId(c.id)}`, type: 'text', props: { heading: word(r) } };
        root.splice(r.int(root.length + 1), 0, b);
        const set = or.keep.get(c.locale) ?? new Set<string>();
        set.add(b.id as string);
        or.keep.set(c.locale, set);
      } else if (k <= 1 || !allBlocks(root).length) {
        const target = r.pick(plain);
        target.list.splice(r.int(target.list.length + 1), 0, newBlock(r, c.id));
      } else if (k === 2) {
        const target = r.pick(plain.filter((l) => l.list.some((b) => !isKeep(b))));
        if (target) {
          const cand = target.list.map((b, i) => [b, i] as const).filter(([b]) => !isKeep(b));
          target.list.splice(r.pick(cand)[1], 1);
        }
      } else if (k === 3) {
        const from = r.pick(plain.filter((l) => l.list.some((b) => !isKeep(b))));
        if (from) {
          const cand = from.list.map((b, i) => [b, i] as const).filter(([b]) => !isKeep(b));
          const [b] = from.list.splice(r.pick(cand)[1], 1);
          const dest = r.pick(lists(root).filter((l) => !l.owner || !hasId(b!, l.owner.id)));
          dest.list.splice(r.int(dest.list.length + 1), 0, b!);
        }
      } else {
        const b = r.pick(allBlocks(root).filter((x) => !isKeep(x)));
        if (b?.type === 'text') {
          const p = (b.props ??= {}) as Doc;
          if (r.chance(0.7)) p.heading = splice(r, (p.heading as string) ?? '');
          else p.size = r.pick(['s', 'm', 'l']);
        } else if (b) ((b.props ??= {}) as Doc).gap = r.int(5);
      }
      break;
    }
    case 11:
      // The narrow-schema rich text of a block (kept simple: replaced as a whole).
      break;
    default:
      if (c.b && pmMode) {
        if (!editPM(r, c, pmMode === 'struct', or)) editRich(r, c);
      } else if (c.b && !pmMode) editRich(r, c);
  }
}

/** Moves a block (found by id) somewhere else in the client's tree. */
function moveById(r: Rng, c: Client, id: string) {
  const root = ((c.v.fields.blocks ??= []) as Doc[]);
  const from = lists(root).find((l) => l.list.some((b) => b.id === id));
  if (!from) return false;
  const [b] = from.list.splice(from.list.findIndex((x) => x.id === id), 1);
  const dest = r.pick(lists(root).filter((l) => !l.owner || !hasId(b!, l.owner.id)));
  dest.list.splice(r.int(dest.list.length + 1), 0, b!);
  return true;
}

// Microtasks only: the fake transports resolve without timers.
async function micro(n = 6) {
  for (let i = 0; i < n; i++) await Promise.resolve();
}

function partsPlain(c: Client): Doc {
  const out: Doc = {};
  for (const p of c.b!.parts) Object.assign(out, toPlain(c.b!.doc.getMap(p)));
  return out;
}

function checkShapes(root: Doc[], seen = new Set<string>(), allow?: string[]) {
  void allow;
  for (const b of root) {
    expect(typeof b.id).toBe('string');
    expect(seen.has(b.id as string), `duplicate block id ${b.id}`).toBe(false);
    seen.add(b.id as string);
    expect(['text', 'columns']).toContain(b.type);
    const p = (b.props ?? {}) as Doc;
    if (b.type === 'text') {
      expect(p.heading === undefined || typeof p.heading === 'string').toBe(true);
      expect(p.size === undefined || ['s', 'm', 'l'].includes(p.size as string)).toBe(true);
    } else expect(p.gap === undefined || typeof p.gap === 'number').toBe(true);
    for (const [k, l] of Object.entries((b.slots ?? {}) as Record<string, Doc[]>)) {
      expect(slotNames[b.type as string]).toContain(k);
      checkShapes(l, seen);
    }
  }
}


interface Run {
  r: Rng;
  server: FakeServer;
  clock: Clock;
  clients: Client[];
  or: Oracle;
  pmMode: '' | 'plain' | 'struct';
  blocked: Set<string>;
  failP: number;
}

const live = (run: Run) => run.clients.filter((c) => c.b);

function newClient(run: Run, locale: string, pre: boolean): Client {
  const t = run.server.connect();
  const c: Client = { id: run.clients.length, locale, t, b: undefined, v: { fields: hydrate(type.fields, structuredClone(draft.fields)) as Doc }, tick: [], fatal: '', own: [], typed: [], seedTyped: [] };
  run.clients.push(c);
  // Edits made between loading the draft and joining the session.
  if (pre) for (let i = run.r.int(4); i > 0; i--) edit(run.r, c, run.or, '');
  return c;
}

async function startBinder(run: Run, c: Client) {
  const join = await c.t.join(c.locale);
  c.b = new Binder({
    transport: c.t,
    join,
    type,
    locale: c.locale,
    draft,
    fields: () => c.v.fields,
    snapshot: (x) => structuredClone(x),
    env,
    schedule: (f) => c.tick.push(f),
    setTimeout: run.clock.setTimeout as never,
    clearTimeout: run.clock.clearTimeout,
    merge: c.merge,
    resume: c.resume,
    onFatal: (reason) => {
      c.fatal = reason;
    },
  });
  c.merge = c.resume = undefined;
  await c.b.started;
}

/** Like the editor: a lost claim starts the session again from the draft, keeping the form's edits to the lost parts. */
async function restart(run: Run, c: Client) {
  c.merge = c.b!.lostParts();
  c.resume = c.b!.ackedValues();
  // A list of groups is merged as a whole on a restart: what this client added or typed may give way to others'
  // changes, and a form object of an item that did not make it may then show another one.
  for (const tok of c.typed.splice(0)) run.or.sectSafe.delete(tok);
  c.own.length = 0;
  // So is rich text typed into the part whose claim was lost.
  for (const tok of c.seedTyped.splice(0)) if (c.merge.includes(`l:${c.locale}`)) (run.or.body.get(c.locale)?.delete(tok), run.or.bodyRisk.add(tok));
  c.b!.destroy({ discard: true });
  c.pm?.view.destroy();
  c.pm = undefined;
  c.fatal = '';
  c.tick.length = 0;
  restarts++;
  await startBinder(run, c);
}

async function settleFatal(run: Run) {
  for (const c of live(run)) if (c.fatal === 'claim_lost') (claimLosts++, await restart(run, c));
}

async function quiesce(run: Run) {
  const { server, clock } = run;
  run.blocked.clear();
  run.failP = 0;
  server.failPost = null;
  for (const c of run.clients) if (!c.b) await startBinder(run, c);
  for (let round = 0; round < 40; round++) {
    let busy = false;
    await settleFatal(run);
    for (const c of live(run)) {
      if (c.tick.splice(0).length) busy = true;
      c.b!.flush();
    }
    await micro();
    // Retry timers (2-15 s), pending-part timers (16 s) and presence-driven re-joins.
    clock.advance(16_000);
    for (const c of live(run)) for (const f of [...c.t.presenceListeners]) f([]);
    await micro();
    for (const c of live(run)) {
      while (c.t.queue.length) {
        busy = true;
        c.t.deliver(run.r.int(c.t.queue.length));
      }
    }
    await micro();
    if (!busy && live(run).every((c) => !c.fatal && !c.b!.unsynced() && c.b!.pending().length === 0 && c.t.queue.length === 0)) return round;
  }
  throw new Error('did not settle');
}

function count(hay: string, tok: string) {
  return hay.split(tok).length - 1;
}

async function run(seed: number) {
  const r = rng(seed);
  const server = new FakeServer();
  server.strict = true;
  const clock = new Clock(server);
  const or: Oracle = { note: new Set(), keep: new Map(), body: new Map(), sect: new Map(), sectSafe: new Set(), bodyRisk: new Set() };
  const pmMode: Run['pmMode'] = seed % 4 === 0 ? (seed % 8 === 0 ? 'struct' : 'plain') : '';
  const R: Run = { r, server, clock, clients: [], or, pmMode, blocked: new Set(), failP: 0 };
  server.failPost = (client: string) => R.blocked.has(client) || (R.failP > 0 && r.chance(R.failP));
  const locales = r.chance(0.5) ? ['en'] : ['en', 'de'];
  const n = 3 + r.int(3);
  for (let i = 0; i < n; i++) newClient(R, locales[i % locales.length]!, i > 0 && r.chance(0.5));
  // The first client joins at once; the others are joiners that complete in later steps.
  await startBinder(R, R.clients[0]!);
  const steps = 25 + r.int(35);
  for (let s = 0; s < steps; s++) {
    await settleFatal(R);
    const lv = live(R);
    const c = r.pick(lv);
    const roll = r.next();
    if (roll < 0.38) {
      edit(r, c, or, pmMode);
      if (r.chance(0.6)) c.b!.flush();
    } else if (roll < 0.46) {
      c.tick.splice(0);
      c.b!.flush();
    } else if (roll < 0.74) {
      if (c.t.queue.length) c.t.deliver(r.int(c.t.queue.length), r.chance(0.15));
      else await micro();
    } else if (roll < 0.77) {
      const ids2 = allBlocks((c.v.fields.blocks ?? []) as Doc[]).filter((b) => !isKeep(b)).map((b) => b.id as string);
      if (ids2.length) {
        const id = r.pick(ids2);
        for (const who of [c, r.pick(lv.filter((x) => x.locale === c.locale))]) if (moveById(r, who, id)) who.b!.flush();
        dups++;
      }
    } else if (roll < 0.82) {
      // A joiner completes its join (the log has moved on since it loaded the draft).
      const j = R.clients.find((x) => !x.b);
      if (j) {
        if (r.chance(0.3)) R.blocked.add(j.t.client);
        await startBinder(R, j);
      } else if (R.clients.length < 6) {
        const nc = newClient(R, r.pick(locales), r.chance(0.5));
        void nc;
      }
    } else if (roll < 0.86) {
      // Time passes: retry timers fire and claims lapse.
      clock.advance(r.pick([500, 3_000, 16_000, 20_000]));
    } else if (roll < 0.9) {
      // A re-join with a replay of the whole log.
      await c.t.rejoin().catch(() => undefined);
    } else if (roll < 0.93) {
      for (const f of [...c.t.presenceListeners]) f([]);
    } else if (roll < 0.96) {
      R.failP = r.pick([0, 0.3, 0.6]);
    } else if (roll < 0.98) {
      if (R.blocked.size) R.blocked.delete(r.pick([...R.blocked]));
      else if (c.b!.unsynced() || r.chance(0.3)) R.blocked.add(c.t.client);
    } else if (roll < 0.995) {
      // Two editors add an (empty, so equal) list item at the same time, typing into it now or later.
      for (const who of new Set([c, r.pick(lv)])) {
        addOwn(r, who, or, r.chance(0.3));
        if (r.chance(0.5)) who.b!.flush();
      }
    } else await micro();
    await micro(2);
  }
  await quiesce(R);
  const lv = live(R);
  const ref = lv[0]!;
  const sv = Y.encodeStateVector(ref.b!.doc);
  for (const [i, c] of lv.entries()) {
    const y = partsPlain(c);
    expect(jsonEq(c.v.fields, y), `seed ${seed}: client ${i} (${c.locale}) values differ from Y\n${JSON.stringify(c.v.fields)}\n${JSON.stringify(y)}`).toBe(true);
    for (const p of ['shared', `l:${c.locale}`]) {
      const peer = lv.find((x) => x !== c && x.b!.parts.includes(p));
      if (peer) expect(jsonEq(toPlain(c.b!.doc.getMap(p)), toPlain(peer.b!.doc.getMap(p))), `seed ${seed}: part ${p} differs between clients`).toBe(true);
    }
    checkShapes((c.v.fields.blocks ?? []) as Doc[]);
    expect(Y.encodeStateVector(c.b!.doc), `seed ${seed}: state vector of client ${i}`).toEqual(sv);
    expect(c.b!.unsynced()).toBe(false);
    // Intent: nothing a client typed that nobody removed is gone, and nothing is doubled.
    const note = ((c.v.fields.meta as Doc | undefined)?.note as string | undefined) ?? '';
    for (const tok of or.note) expect(count(note, tok), `seed ${seed}: note token ${tok.codePointAt(0)!.toString(16)} in client ${i}`).toBe(1);
    const blocks = ((c.v.fields.blocks ?? []) as Doc[]).map((b) => b.id as string);
    for (const id of or.keep.get(c.locale) ?? []) expect(blocks.filter((x) => x === id).length, `seed ${seed}: keep block ${id} in client ${i}`).toBe(1);
    const heads = ((c.v.fields.sections ?? []) as Doc[]).map((x) => (x.heading as string | undefined) ?? '');
    for (const [tok] of or.sect) {
      const n = count(heads.join('\n'), tok);
      if (or.sectSafe.has(tok)) expect(n, `seed ${seed}: list token ${tok.codePointAt(0)!.toString(16)} in client ${i}\n${JSON.stringify(heads)}`).toBe(1);
      else expect(n, `seed ${seed}: list token doubled in client ${i}`).toBeLessThanOrEqual(1);
    }
    for (const h of heads) {
      const keys = new Set([...h].filter((ch) => or.sect.has(ch)).map((ch) => or.sect.get(ch)));
      expect(keys.size, `seed ${seed}: tokens of different list items in one item of client ${i}: ${JSON.stringify(heads)}`).toBeLessThanOrEqual(1);
    }
    // The form object an editor added (in the session) still holds that item, not one somebody else added.
    for (const o of c.own) {
      if (!o.safe || !((c.v.fields.sections ?? []) as Doc[]).includes(o.item)) continue;
      for (const ch of (o.item.heading as string | undefined) ?? '') {
        if (or.sect.has(ch)) expect(or.sect.get(ch), `seed ${seed}: client ${i} shows another item in the one it added`).toBe(o.key);
      }
    }
    if (pmMode) {
      const body = JSON.stringify(c.v.fields.body ?? '');
      if (pmMode === 'plain') for (const tok of or.body.get(c.locale) ?? []) expect(count(body, tok), `seed ${seed}: body token in client ${i}`).toBe(1);
      for (const tok of or.bodyRisk) expect(count(body, tok), `seed ${seed}: body token doubled in client ${i}`).toBeLessThanOrEqual(1);
    }
  }
  for (const c of R.clients) {
    c.pm?.view.destroy();
    c.b?.destroy();
  }
}

describe('collaboration convergence', () => {
  it(`converges for ${RUNS} seeded runs`, async () => {
    const t0 = performance.now();
    const only = Number(process.env.COLLAB_FUZZ_SEED ?? 0);
    for (let seed = only || 1; seed <= (only || RUNS); seed++) {
      try {
        await run(seed);
      } catch (e) {
        throw new Error(`seed ${seed}: ${(e as Error).message}`, { cause: e });
      }
    }
    const ms = performance.now() - t0;
    console.log(`collab fuzz: ${RUNS} runs in ${Math.round(ms)} ms, ${dups} concurrent block moves, ${claimLosts} lost claims, ${restarts} restarts, ${pmEdits} ProseMirror edits`);
    expect(ms).toBeLessThan(40_000);
  }, 120_000);
});
