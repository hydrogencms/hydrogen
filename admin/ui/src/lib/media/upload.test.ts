import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { setHandlers } from '../api/client';
import { UPLOAD_TIMEOUT, uploadFile } from './upload';

class FakeXHR {
  static all: FakeXHR[] = [];
  upload: { onprogress?: (e: unknown) => void } = {};
  headers: Record<string, string> = {};
  timeout = 0;
  status = 0;
  statusText = '';
  responseText = '';
  withCredentials = false;
  aborted = false;
  onload?: () => void;
  onerror?: () => void;
  onabort?: () => void;
  ontimeout?: () => void;
  constructor() {
    FakeXHR.all.push(this);
  }
  open() {}
  setRequestHeader(k: string, v: string) {
    this.headers[k] = v;
  }
  send() {}
  abort() {
    this.aborted = true;
    this.onabort?.();
  }
  reply(status: number, body: unknown) {
    this.status = status;
    this.responseText = JSON.stringify(body);
    this.onload?.();
  }
}

const f = new File(['x'], 'a.png', { type: 'image/png' });
const ok = { file: { id: 'ab', name: 'a.png', mime: 'image/png', size: 1 } };

beforeEach(() => {
  FakeXHR.all = [];
  vi.stubGlobal('XMLHttpRequest', FakeXHR);
  setHandlers({ csrf: () => 'tok' });
});
afterEach(() => {
  vi.unstubAllGlobals();
  setHandlers({});
});

describe('uploadFile', () => {
  it('does nothing when the signal is already aborted', async () => {
    const ctl = new AbortController();
    ctl.abort();
    await expect(uploadFile(f, { signal: ctl.signal })).rejects.toMatchObject({ name: 'AbortError' });
    expect(FakeXHR.all).toHaveLength(0);
  });

  it('sets a timeout and the CSRF token, and does not keep the abort listener after finishing', async () => {
    const ctl = new AbortController();
    const remove = vi.spyOn(ctl.signal, 'removeEventListener');
    const p = uploadFile(f, { signal: ctl.signal });
    const x = FakeXHR.all[0]!;
    expect(x.timeout).toBe(UPLOAD_TIMEOUT);
    expect(x.headers['X-CSRF-Token']).toBe('tok');
    x.reply(201, ok);
    await expect(p).resolves.toMatchObject({ existing: false });
    expect(remove).toHaveBeenCalledWith('abort', expect.any(Function));
    ctl.abort();
    expect(x.aborted).toBe(false);
  });

  it('reports a timeout as a status 0 error', async () => {
    const p = uploadFile(f);
    FakeXHR.all[0]!.ontimeout?.();
    await expect(p).rejects.toMatchObject({ status: 0, code: 'timeout' });
  });

  it('treats a 401 like the API client does', async () => {
    const unauthorized = vi.fn();
    setHandlers({ unauthorized });
    const p = uploadFile(f);
    FakeXHR.all[0]!.reply(401, { error: { code: 'unauthorized', message: 'no' } });
    await expect(p).rejects.toMatchObject({ status: 401 });
    expect(unauthorized).toHaveBeenCalled();
  });
});
