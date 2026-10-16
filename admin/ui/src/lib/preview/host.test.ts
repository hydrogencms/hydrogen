import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest';
import { PreviewHost, sha256Hex, type HostStatus } from './host';

const ORIGIN = 'https://front.example';
const TOKEN = 'tok.en';
let HASH = '';
beforeAll(async () => {
  HASH = await sha256Hex(TOKEN);
});
// Lets the host's SHA-256 comparison settle.
const realTimeout = setTimeout;
const settle = async () => {
  await new Promise<void>((r) => realTimeout(r, 10));
  for (let i = 0; i < 20; i++) await Promise.resolve();
};
const doc = (title: string, body: unknown[] = []) => ({ fields: { title, body } });

function setup(over: Partial<ConstructorParameters<typeof PreviewHost>[0]> = {}) {
  const sent: { msg: any; origin: string }[] = [];
  const win = { postMessage: (msg: unknown, origin: string) => sent.push({ msg, origin }) } as unknown as Window;
  const statuses: HostStatus[] = [];
  const host = new PreviewHost({ origin: ORIGIN, token: TOKEN, frame: () => win, item: 'it1', itemType: 'page', locale: 'en', onStatus: (s) => statuses.push(s), ...over });
  const from = async (data: any, o: { origin: string; source: unknown } = { origin: over.locked ? 'null' : ORIGIN, source: win }) => {
    if (data && data.type === 'ready' && !('preview' in data)) data = { ...data, preview: HASH };
    host.handle({ ...o, data });
    await settle();
  };
  return { host, win, sent, statuses, from };
}

describe('PreviewHost', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('answers ready with init in live mode, to the frame origin only', async () => {
    const { host, sent, from } = setup();
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live', sdk: 'x', url: ORIGIN + '/' });
    expect(sent).toHaveLength(1);
    expect(sent[0]!.origin).toBe(ORIGIN);
    expect(sent[0]!.msg).toEqual({ h: 1, type: 'init', item: 'it1', itemType: 'page', locale: 'en', rev: 1, doc: doc('A') });
  });

  it('ignores other origins, other windows and other versions', async () => {
    const { host, sent, from, win } = setup();
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live' }, { origin: 'https://evil.example', source: win });
    await from({ h: 1, type: 'ready', mode: 'live' }, { origin: ORIGIN, source: {} });
    await from({ h: 2, type: 'ready', mode: 'live' });
    await from('ready');
    expect(sent).toHaveLength(0);
  });

  it('sends one debounced patch per burst of edits, with base and rev', async () => {
    const { host, sent, from } = setup();
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live' });
    host.setDoc(doc('AB'));
    host.setDoc(doc('ABC'));
    expect(sent).toHaveLength(1);
    vi.advanceTimersByTime(60);
    expect(sent).toHaveLength(2);
    expect(sent[1]!.msg).toEqual({ h: 1, type: 'patch', rev: 2, base: 1, ops: [{ op: 'replace', path: '/fields/title', value: 'ABC' }] });
    host.setDoc(doc('ABC'));
    vi.advanceTimersByTime(100);
    expect(sent).toHaveLength(2);
  });

  it('answers resync with a fresh init', async () => {
    const { host, sent, from } = setup();
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live' });
    host.setDoc(doc('B'));
    await from({ h: 1, type: 'resync', rev: -1 });
    const last = sent[sent.length - 1]!.msg;
    expect(last.type).toBe('init');
    expect(last.doc).toEqual(doc('B'));
    expect(last.rev).toBe(2);
  });

  it('reports rendered latency and errors', async () => {
    const rendered: number[] = [];
    const { from, statuses } = setup({ onRendered: (ms) => rendered.push(ms) });
    await from({ h: 1, type: 'ready', mode: 'live' });
    await from({ h: 1, type: 'rendered', rev: 1, ms: 12 });
    await from({ h: 1, type: 'error', message: 'boom' });
    expect(rendered).toEqual([12]);
    expect(statuses.map((s) => s.phase)).toEqual(['live', 'live', 'error']);
    expect(statuses.at(-1)!.message).toBe('boom');
  });

  it('reload mode: no init, autosave after the pause, then reload; repeated ready is not a reset', async () => {
    const save = vi.fn(async () => true);
    const { host, sent, from } = setup({ save });
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'reload' });
    await from({ h: 1, type: 'ready', mode: 'reload' });
    expect(sent).toHaveLength(0);
    expect(host.mode).toBe('reload');
    host.setDoc(doc('B'));
    host.setDoc(doc('BC'));
    vi.advanceTimersByTime(500);
    expect(save).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(150);
    expect(save).toHaveBeenCalledTimes(1);
    expect(sent.map((s) => s.msg.type)).toEqual(['reload']);
    await from({ h: 1, type: 'ready', mode: 'reload' });
    host.setDoc(doc('BC'));
    await vi.advanceTimersByTimeAsync(1000);
    expect(save).toHaveBeenCalledTimes(1);
  });

  it('reload mode: a failed save sends nothing, and edits during a save are saved next', async () => {
    let n = 0;
    let release!: () => void;
    const save = vi.fn(() => (n++ === 0 ? Promise.resolve(false) : new Promise<boolean>((r) => (release = () => r(true)))));
    const { host, sent, from } = setup({ save });
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'reload' });
    host.setDoc(doc('B'));
    await vi.advanceTimersByTimeAsync(700);
    expect(sent).toHaveLength(0);
    host.setDoc(doc('C'));
    await vi.advanceTimersByTimeAsync(700);
    host.setDoc(doc('D'));
    await vi.advanceTimersByTimeAsync(700);
    release();
    await vi.advanceTimersByTimeAsync(10);
    expect(sent.filter((s) => s.msg.type === 'reload').length).toBeGreaterThanOrEqual(1);
    expect(save.mock.calls.length).toBeGreaterThanOrEqual(2);
  });
});

describe('PreviewHost ordering', () => {
  it('sends init when the document arrives after ready', async () => {
    const { host, sent, from } = setup();
    await from({ h: 1, type: 'ready', mode: 'live' });
    expect(sent).toHaveLength(0);
    host.setDoc(doc('Late'));
    expect(sent).toHaveLength(1);
    expect(sent[0]!.msg).toMatchObject({ type: 'init', rev: 1, doc: doc('Late') });
  });

  it('a frame load without a ready since the last load means no bridge', async () => {
    const { host, from, statuses } = setup();
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live' });
    host.frameLoaded(); // the load of the page that sent that ready
    expect(host.mode).toBe('live');
    host.frameLoaded(); // a navigation to a page without a bridge
    expect(host.mode).toBeNull();
    expect(statuses.at(-1)!.phase).toBe('disconnected');
    await from({ h: 1, type: 'ready', mode: 'live' });
    expect(statuses.at(-1)!.phase).toBe('live');
  });
});

describe('PreviewHost locked frames', () => {
  it('accepts by source alone (origin "null") and posts with target *', async () => {
    const { host, sent, from, win } = setup({ locked: true });
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live' }, { origin: ORIGIN, source: win });
    await from({ h: 1, type: 'ready', mode: 'live' }, { origin: 'null', source: {} });
    expect(sent).toHaveLength(1);
    expect(sent[0]!.origin).toBe('*');
    expect(sent[0]!.msg.type).toBe('init');
  });

  it('unlocked frames still need the preview origin and post to it', async () => {
    const { host, sent, from, win } = setup();
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live' }, { origin: 'null', source: win });
    expect(sent).toHaveLength(0);
    await from({ h: 1, type: 'ready', mode: 'live' });
    expect(sent.map((s) => s.origin)).toEqual([ORIGIN]);
  });
});

describe('PreviewHost proof', () => {
  it('sends init only when ready carries the hash of the minted token', async () => {
    const { host, sent, from, statuses } = setup();
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live', preview: 'f'.repeat(64) });
    await from({ h: 1, type: 'ready', mode: 'live', preview: undefined });
    await from({ h: 1, type: 'ready', mode: 'live', preview: 42 });
    expect(sent).toHaveLength(0);
    expect(host.mode).toBeNull();
    expect(statuses.at(-1)!.phase).toBe('unverified');
    await from({ h: 1, type: 'ready', mode: 'live', preview: HASH });
    expect(sent.map((s) => s.msg.type)).toEqual(['init']);
    expect(statuses.at(-1)!.phase).toBe('live');
  });

  it('a mismatch after a good ready stops the exchange; other messages are ignored until verified', async () => {
    vi.useFakeTimers();
    const { host, sent, from } = setup();
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live' });
    await from({ h: 1, type: 'ready', mode: 'live', preview: 'nope' });
    host.setDoc(doc('B'));
    await from({ h: 1, type: 'resync', rev: -1 });
    vi.advanceTimersByTime(200);
    expect(sent.map((s) => s.msg.type)).toEqual(['init']);
    vi.useRealTimers();
  });

  it('reload mode re-verifies each ready', async () => {
    vi.useFakeTimers();
    const save = vi.fn(async () => true);
    const { host, sent, from, statuses } = setup({ save });
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'reload' });
    host.setDoc(doc('B'));
    await vi.advanceTimersByTimeAsync(700);
    expect(sent.map((s) => s.msg.type)).toEqual(['reload']);
    await from({ h: 1, type: 'ready', mode: 'reload', preview: 'other' });
    expect(statuses.at(-1)!.phase).toBe('unverified');
    expect(host.mode).toBeNull();
    vi.useRealTimers();
  });
});

describe('PreviewHost locked reload', () => {
  it('completes on the load event plus a verified ready, in either order, and reports the latency', async () => {
    vi.useFakeTimers();
    const rendered: number[] = [];
    const { host, from, win } = setup({ locked: true, save: async () => true, onRendered: (ms) => rendered.push(ms) });
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'reload' });
    host.frameLoaded();
    for (const order of ['loadFirst', 'readyFirst']) {
      host.setDoc(doc(order));
      await vi.advanceTimersByTimeAsync(700);
      vi.advanceTimersByTime(40);
      expect(rendered.length).toBe(order === 'loadFirst' ? 0 : 1);
      if (order === 'loadFirst') {
        host.frameLoaded();
        expect(rendered).toHaveLength(0);
        await from({ h: 1, type: 'ready', mode: 'reload' }, { origin: 'null', source: win });
        expect(rendered).toHaveLength(1);
        expect(host.mode).toBe('reload');
      } else {
        await from({ h: 1, type: 'ready', mode: 'reload' }, { origin: 'null', source: win });
        expect(rendered).toHaveLength(1);
        host.frameLoaded();
        expect(rendered).toHaveLength(2);
      }
    }
    vi.useRealTimers();
  });

  it('an unverified ready does not complete the reload', async () => {
    vi.useFakeTimers();
    const rendered: number[] = [];
    const { host, from } = setup({ locked: true, save: async () => true, onRendered: (ms) => rendered.push(ms) });
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'reload' });
    host.setDoc(doc('B'));
    await vi.advanceTimersByTimeAsync(700);
    host.frameLoaded();
    await from({ h: 1, type: 'ready', mode: 'reload', preview: 'x' });
    expect(rendered).toHaveLength(0);
    vi.useRealTimers();
  });
});

describe('PreviewHost canvas editing', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());
  const texts = [{ path: 'fields/title', text: 'Home', scope: '' }];
  const overlays = (sent: { msg: any }[]) => sent.filter((s) => s.msg.type === 'overlay');

  it('sends overlay after a verified ready, after init, and again after each ready', async () => {
    let on = true;
    const { host, sent, from } = setup({ overlay: () => ({ on, texts }) });
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live', edit: true });
    expect(sent.map((s) => s.msg.type)).toEqual(['init', 'overlay']);
    expect(sent[1]!.msg).toEqual({ h: 1, type: 'overlay', on: true, texts });
    await from({ h: 1, type: 'ready', mode: 'live', edit: true });
    expect(overlays(sent)).toHaveLength(2);
    on = false;
    await from({ h: 1, type: 'ready', mode: 'live' });
    expect(overlays(sent)[2]!.msg.on).toBe(false);
  });

  it('does not send overlay before verification; setOverlay before ready is sent after it', async () => {
    const { host, sent, from } = setup();
    host.setOverlay(true, texts);
    expect(sent).toHaveLength(0);
    await from({ h: 1, type: 'ready', mode: 'live', preview: 'bad' });
    expect(sent).toHaveLength(0);
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'reload', edit: true });
    expect(overlays(sent)).toHaveLength(1);
  });

  it('setOverlay dedupes by deep comparison', async () => {
    const { host, sent, from } = setup();
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live', edit: true });
    host.setOverlay(true, texts);
    host.setOverlay(true, [{ ...texts[0]! }]);
    expect(overlays(sent)).toHaveLength(1);
    host.setOverlay(true, [{ path: 'fields/title', text: 'Other', scope: '' }]);
    host.setOverlay(false, []);
    expect(overlays(sent)).toHaveLength(3);
  });

  it('tracks editCapable from each verified ready', async () => {
    const caps: boolean[] = [];
    const { host, from } = setup({ onCapabilities: (e) => caps.push(e) });
    expect(host.editCapable).toBe(false);
    await from({ h: 1, type: 'ready', mode: 'live', edit: true });
    expect(host.editCapable).toBe(true);
    await from({ h: 1, type: 'ready', mode: 'live' });
    expect(host.editCapable).toBe(false);
    await from({ h: 1, type: 'ready', mode: 'live', edit: true });
    await from({ h: 1, type: 'ready', mode: 'live', edit: true, preview: 'bad' });
    expect(host.editCapable).toBe(false);
    expect(caps).toEqual([true, false, true, false]);
  });

  it('routes layout, hover, select and inlineEdit only from the verified frame', async () => {
    const layouts: any[] = [];
    const hovers: any[] = [];
    const selects: any[] = [];
    const edits: any[] = [];
    const o = { onLayout: (n: any, v: any) => layouts.push([n, v]), onHover: (p: any) => hovers.push(p), onSelect: (p: any, e: any) => selects.push([p, e]), onInlineEdit: (p: any, v: any) => edits.push([p, v]) };
    const { host, from, win } = setup(o);
    host.setDoc(doc('A'));
    const nodes = [{ path: 'fields/body/0', rect: [1, 2, 3, 4], block: 'hero', text: true }];
    const all = async (origin?: string, source?: unknown) => {
      const opt = origin ? { origin, source: source ?? win } : undefined;
      await from({ h: 1, type: 'layout', nodes, vw: 800, vh: 600 }, opt);
      await from({ h: 1, type: 'hover', path: 'fields/body/0' }, opt);
      await from({ h: 1, type: 'select', path: 'fields/body/0', edit: true }, opt);
      await from({ h: 1, type: 'inlineEdit', path: 'fields/title', value: 'X' }, opt);
    };
    await all(); // not verified yet
    expect(layouts.length + hovers.length + selects.length + edits.length).toBe(0);
    await from({ h: 1, type: 'ready', mode: 'live', edit: true });
    layouts.length = 0;
    await all('https://evil.example');
    await all(ORIGIN, {});
    expect(layouts.length + hovers.length + selects.length + edits.length).toBe(0);
    await all();
    expect(layouts).toEqual([[nodes, { w: 800, h: 600 }]]);
    expect(hovers).toEqual(['fields/body/0']);
    expect(selects).toEqual([['fields/body/0', true]]);
    expect(edits).toEqual([['fields/title', 'X']]);
    await from({ h: 1, type: 'hover', path: null });
    await from({ h: 1, type: 'select', path: null, edit: false });
    await from({ h: 1, type: 'inlineEdit', path: 'fields/title', cancel: true });
    expect(hovers.at(-1)).toBeNull();
    expect(selects.at(-1)).toEqual([null, false]);
    expect(edits.at(-1)).toEqual(['fields/title', null]);
  });

  it('drops malformed canvas messages', async () => {
    const calls: unknown[] = [];
    const { host, from } = setup({ onLayout: (n) => calls.push(n), onHover: (p) => calls.push(p), onSelect: (p) => calls.push(p), onInlineEdit: (p) => calls.push(p) });
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live', edit: true });
    calls.length = 0;
    await from({ h: 1, type: 'layout', nodes: 'x', vw: 1, vh: 1 });
    await from({ h: 1, type: 'layout', nodes: [{ path: 'a', rect: [1, 2, 3] }], vw: 1, vh: 1 });
    await from({ h: 1, type: 'layout', nodes: [], vw: 'x', vh: 1 });
    await from({ h: 1, type: 'hover', path: 4 });
    await from({ h: 1, type: 'select', path: 4, edit: false });
    await from({ h: 1, type: 'inlineEdit', path: 'a' });
    await from({ h: 1, type: 'inlineEdit', value: 'a' });
    expect(calls).toEqual([]);
  });

  it('clears the layout when unverified or disconnected', async () => {
    const layouts: any[] = [];
    const { host, from } = setup({ onLayout: (n, v) => layouts.push([n, v]) });
    host.setDoc(doc('A'));
    await from({ h: 1, type: 'ready', mode: 'live', edit: true });
    await from({ h: 1, type: 'ready', mode: 'live', preview: 'bad' });
    expect(layouts).toEqual([[[], { w: 0, h: 0 }]]);
    await from({ h: 1, type: 'ready', mode: 'live', edit: true });
    host.frameLoaded(); // consumes the ready
    host.frameLoaded(); // no ready since: no bridge
    expect(layouts).toHaveLength(2);
    expect(host.editCapable).toBe(false);
  });

  it('posts scrollTo and editText to the frame origin, or * when locked', async () => {
    for (const locked of [false, true]) {
      const { host, sent, from } = setup({ locked });
      host.scrollTo('fields/title');
      host.editText('fields/title', false);
      expect(sent).toHaveLength(0);
      host.setDoc(doc('A'));
      await from({ h: 1, type: 'ready', mode: 'live', edit: true });
      sent.length = 0;
      host.scrollTo('fields/body/1');
      host.scrollTo({ by: -16 });
      host.editText('fields/title', true);
      expect(sent.map((s) => s.msg)).toEqual([
        { h: 1, type: 'scrollTo', path: 'fields/body/1' },
        { h: 1, type: 'scrollTo', by: -16 },
        { h: 1, type: 'editText', path: 'fields/title', multiline: true },
      ]);
      expect(new Set(sent.map((s) => s.origin))).toEqual(new Set([locked ? '*' : ORIGIN]));
    }
  });
});
