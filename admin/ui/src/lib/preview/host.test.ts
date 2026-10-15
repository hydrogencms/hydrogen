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
  await new Promise<void>((r) => realTimeout(r, 3));
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
