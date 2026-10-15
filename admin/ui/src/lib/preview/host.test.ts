import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PreviewHost, type HostStatus } from './host';

const ORIGIN = 'https://front.example';
const doc = (title: string, body: unknown[] = []) => ({ fields: { title, body } });

function setup(over: Partial<ConstructorParameters<typeof PreviewHost>[0]> = {}) {
  const sent: { msg: any; origin: string }[] = [];
  const win = { postMessage: (msg: unknown, origin: string) => sent.push({ msg, origin }) } as unknown as Window;
  const statuses: HostStatus[] = [];
  const host = new PreviewHost({ origin: ORIGIN, frame: () => win, item: 'it1', itemType: 'page', locale: 'en', onStatus: (s) => statuses.push(s), ...over });
  const from = (data: unknown, o = { origin: ORIGIN, source: win as unknown }) => host.handle({ ...o, data });
  return { host, win, sent, statuses, from };
}

describe('PreviewHost', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('answers ready with init in live mode, to the frame origin only', () => {
    const { host, sent, from } = setup();
    host.setDoc(doc('A'));
    from({ h: 1, type: 'ready', mode: 'live', sdk: 'x', url: ORIGIN + '/' });
    expect(sent).toHaveLength(1);
    expect(sent[0]!.origin).toBe(ORIGIN);
    expect(sent[0]!.msg).toEqual({ h: 1, type: 'init', item: 'it1', itemType: 'page', locale: 'en', rev: 1, doc: doc('A') });
  });

  it('ignores other origins, other windows and other versions', () => {
    const { host, sent, from, win } = setup();
    host.setDoc(doc('A'));
    from({ h: 1, type: 'ready', mode: 'live' }, { origin: 'https://evil.example', source: win });
    from({ h: 1, type: 'ready', mode: 'live' }, { origin: ORIGIN, source: {} });
    from({ h: 2, type: 'ready', mode: 'live' });
    from('ready');
    expect(sent).toHaveLength(0);
  });

  it('sends one debounced patch per burst of edits, with base and rev', () => {
    const { host, sent, from } = setup();
    host.setDoc(doc('A'));
    from({ h: 1, type: 'ready', mode: 'live' });
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

  it('answers resync with a fresh init', () => {
    const { host, sent, from } = setup();
    host.setDoc(doc('A'));
    from({ h: 1, type: 'ready', mode: 'live' });
    host.setDoc(doc('B'));
    from({ h: 1, type: 'resync', rev: -1 });
    const last = sent[sent.length - 1]!.msg;
    expect(last.type).toBe('init');
    expect(last.doc).toEqual(doc('B'));
    expect(last.rev).toBe(2);
  });

  it('reports rendered latency and errors', () => {
    const rendered: number[] = [];
    const { from, statuses } = setup({ onRendered: (ms) => rendered.push(ms) });
    from({ h: 1, type: 'ready', mode: 'live' });
    from({ h: 1, type: 'rendered', rev: 1, ms: 12 });
    from({ h: 1, type: 'error', message: 'boom' });
    expect(rendered).toEqual([12]);
    expect(statuses.map((s) => s.phase)).toEqual(['live', 'live', 'error']);
    expect(statuses.at(-1)!.message).toBe('boom');
  });

  it('reload mode: no init, autosave after the pause, then reload; repeated ready is not a reset', async () => {
    const save = vi.fn(async () => true);
    const { host, sent, from } = setup({ save });
    host.setDoc(doc('A'));
    from({ h: 1, type: 'ready', mode: 'reload' });
    from({ h: 1, type: 'ready', mode: 'reload' });
    expect(sent).toHaveLength(0);
    expect(host.mode).toBe('reload');
    host.setDoc(doc('B'));
    host.setDoc(doc('BC'));
    vi.advanceTimersByTime(500);
    expect(save).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(150);
    expect(save).toHaveBeenCalledTimes(1);
    expect(sent.map((s) => s.msg.type)).toEqual(['reload']);
    from({ h: 1, type: 'ready', mode: 'reload' });
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
    from({ h: 1, type: 'ready', mode: 'reload' });
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
  it('sends init when the document arrives after ready', () => {
    const { host, sent, from } = setup();
    from({ h: 1, type: 'ready', mode: 'live' });
    expect(sent).toHaveLength(0);
    host.setDoc(doc('Late'));
    expect(sent).toHaveLength(1);
    expect(sent[0]!.msg).toMatchObject({ type: 'init', rev: 1, doc: doc('Late') });
  });

  it('a frame load without a ready since the last load means no bridge', () => {
    const { host, from, statuses } = setup();
    host.setDoc(doc('A'));
    from({ h: 1, type: 'ready', mode: 'live' });
    host.frameLoaded(); // the load of the page that sent that ready
    expect(host.mode).toBe('live');
    host.frameLoaded(); // a navigation to a page without a bridge
    expect(host.mode).toBeNull();
    expect(statuses.at(-1)!.phase).toBe('disconnected');
    from({ h: 1, type: 'ready', mode: 'live' });
    expect(statuses.at(-1)!.phase).toBe('live');
  });
});
