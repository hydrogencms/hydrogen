import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError, setHandlers } from './client';

const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json', ...headers } });

let fetchMock: ReturnType<typeof vi.fn>;
beforeEach(() => {
  fetchMock = vi.fn();
  vi.stubGlobal('fetch', fetchMock);
  setHandlers({ csrf: () => 'tok' });
});
afterEach(() => vi.unstubAllGlobals());

describe('api client', () => {
  it('builds the URL with path params and query, same-origin credentials, no CSRF on GET', async () => {
    fetchMock.mockResolvedValue(json(200, { items: [] }));
    await api.get('/users', { query: { limit: 5, offset: undefined } });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('/api/manage/v1/users?limit=5');
    expect(init.credentials).toBe('same-origin');
    expect(init.headers['X-CSRF-Token']).toBeUndefined();

    fetchMock.mockResolvedValue(json(200, {}));
    await api.get('/users/{id}', { params: { id: 'a/b' } });
    expect(fetchMock.mock.calls[1][0]).toBe('/api/manage/v1/users/a%2Fb');
  });

  it('sends the CSRF token and a JSON body on unsafe methods', async () => {
    fetchMock.mockResolvedValue(json(200, { csrf: 'x' }));
    await api.post('/auth/login', { body: { login: 'a', password: 'b' } });
    const init = fetchMock.mock.calls[0][1];
    expect(init.method).toBe('POST');
    expect(init.headers['X-CSRF-Token']).toBe('tok');
    expect(init.headers['Content-Type']).toBe('application/json');
    expect(init.body).toBe('{"login":"a","password":"b"}');
  });

  it('parses the error envelope into ApiError', async () => {
    fetchMock.mockResolvedValue(
      json(422, { error: { code: 'invalid', message: 'bad', fields: [{ path: 'fields.title', code: 'required', message: 'Required' }] } }),
    );
    const err = await api.get('/users').catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 422, code: 'invalid', message: 'bad' });
    expect(err.fields[0].path).toBe('fields.title');
  });

  it('reads Retry-After on throttling', async () => {
    fetchMock.mockResolvedValue(json(429, { error: { code: 'throttled', message: 'slow' } }, { 'Retry-After': '17' }));
    const err = await api.post('/auth/login', { body: { login: 'a', password: 'b' }, noRedirect: true }).catch((e) => e);
    expect(err.retryAfter).toBe(17);
  });

  it('401 calls the unauthorized handler unless noRedirect', async () => {
    const unauthorized = vi.fn();
    setHandlers({ unauthorized });
    fetchMock.mockImplementation(async () => json(401, { error: { code: 'unauthorized', message: 'no' } }));
    await expect(api.get('/users')).rejects.toMatchObject({ status: 401 });
    expect(unauthorized).toHaveBeenCalledTimes(1);
    await expect(api.get('/auth/session', { noRedirect: true })).rejects.toMatchObject({ status: 401 });
    expect(unauthorized).toHaveBeenCalledTimes(1);
  });

  it('5xx calls the server error handler; network failure becomes status 0', async () => {
    const serverError = vi.fn();
    setHandlers({ serverError });
    fetchMock.mockResolvedValueOnce(json(500, { error: { code: 'internal', message: 'oops' } }));
    await expect(api.get('/users')).rejects.toMatchObject({ status: 500 });
    fetchMock.mockRejectedValueOnce(new TypeError('failed'));
    await expect(api.get('/users')).rejects.toMatchObject({ status: 0, code: 'network' });
    expect(serverError).toHaveBeenCalledTimes(2);
  });

  it('handles 204 and non-JSON error bodies', async () => {
    fetchMock.mockResolvedValueOnce(new Response(null, { status: 204 }));
    expect(await api.del('/users/{id}', { params: { id: '1' } })).toBeUndefined();
    fetchMock.mockResolvedValueOnce(new Response('<html>', { status: 502, statusText: 'Bad Gateway' }));
    await expect(api.get('/users')).rejects.toMatchObject({ status: 502, code: 'error' });
  });

  it('sends If-Match, reports response headers, and exposes the ETag of a 412', async () => {
    const seen: string[] = [];
    fetchMock.mockResolvedValueOnce(json(200, {}, { ETag: '"v2"' }));
    await api.patch('/items/{id}', { params: { id: '1' }, body: {} as never, ifMatch: '"v1"', onHeaders: (h) => seen.push(h.get('ETag')!) });
    expect(fetchMock.mock.calls[0][1].headers['If-Match']).toBe('"v1"');
    expect(seen).toEqual(['"v2"']);

    fetchMock.mockResolvedValueOnce(
      json(412, { error: { code: 'precondition_failed', message: 'changed', current: { updatedAt: 't', updatedBy: 'ed' } } }, { ETag: '"v3"' }),
    );
    const err = await api.patch('/items/{id}', { params: { id: '1' }, body: {} as never, ifMatch: '"v1"' }).catch((e) => e);
    expect(err).toMatchObject({ status: 412, code: 'precondition_failed', etag: '"v3"' });
  });
});
