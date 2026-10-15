import type { components, paths } from './schema';
import { config } from '../config';

export type FieldError = components['schemas']['FieldError'];

export class ApiError extends Error {
  status: number;
  code: string;
  fields?: FieldError[];
  retryAfter?: number;
  /** The ETag header of the response (a 412 carries the current one). */
  etag?: string;
  body?: Record<string, unknown>;
  constructor(status: number, code: string, message: string, extra: Partial<ApiError> = {}) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    Object.assign(this, extra);
  }
}

type Method = 'get' | 'post' | 'put' | 'patch' | 'delete';
export type Path<M extends Method> = {
  [P in keyof paths]: paths[P] extends Record<M, unknown> ? P : never;
}[keyof paths];
type Op<P extends keyof paths, M extends Method> = paths[P] extends Record<M, infer O> ? O : never;
type QueryOf<O> = O extends { parameters: { query?: infer Q } } ? Exclude<Q, undefined> : never;
type BodyOf<O> = O extends { requestBody?: infer R }
  ? R extends { content: { 'application/json': infer B } }
    ? B
    : never
  : never;
type ResOf<O> = O extends { responses: infer R }
  ? R extends { 200: { content: { 'application/json': infer B } } }
    ? B
    : R extends { 201: { content: { 'application/json': infer B } } }
      ? B
      : void
  : void;

export interface Options<Q = never, B = never> {
  query?: [Q] extends [never] ? Record<string, never> : Q;
  body?: [B] extends [never] ? undefined : B;
  params?: Record<string, string | number>;
  signal?: AbortSignal;
  /** Do not treat a 401 as "signed out" (login, session probe). */
  noRedirect?: boolean;
  /** Sent as If-Match (optimistic concurrency): the ETag read earlier. */
  ifMatch?: string;
  /** Called with the response headers of a successful call (e.g. to keep the item's new ETag). */
  onHeaders?: (h: Headers) => void;
}

export interface Handlers {
  unauthorized?: () => void;
  serverError?: (e: ApiError) => void;
  csrf?: () => string | undefined;
}

let handlers: Handlers = {};
export function setHandlers(h: Handlers) {
  handlers = h;
}

function buildURL(path: string, params?: Record<string, string | number>, query?: object): string {
  let p = path.replace(/\{(\w+)\}/g, (_, k) => {
    const v = params?.[k];
    if (v === undefined) throw new Error(`missing path param ${k} for ${path}`);
    return encodeURIComponent(String(v));
  });
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(query ?? {})) {
    if (v === undefined || v === null || v === '') continue;
    if (Array.isArray(v)) for (const x of v) qs.append(k, String(x));
    else qs.append(k, String(v));
  }
  const s = qs.toString();
  return config().apiPrefix + p + (s ? '?' + s : '');
}

async function request(method: string, path: string, o: Options<any, any> = {}): Promise<any> {
  const headers: Record<string, string> = { Accept: 'application/json' };
  const unsafe = method !== 'GET';
  if (unsafe) {
    const token = handlers.csrf?.();
    if (token) headers['X-CSRF-Token'] = token;
  }
  if (o.ifMatch) headers['If-Match'] = o.ifMatch;
  let body: string | undefined;
  if (o.body !== undefined) {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(o.body);
  }
  let res: Response;
  try {
    res = await fetch(buildURL(path, o.params, o.query), {
      method,
      headers,
      body,
      credentials: 'same-origin',
      signal: o.signal,
    });
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e;
    const err = new ApiError(0, 'network', 'network error');
    handlers.serverError?.(err);
    throw err;
  }
  if (res.ok) {
    o.onHeaders?.(res.headers);
    if (res.status === 204) return undefined;
    const text = await res.text();
    return text ? JSON.parse(text) : undefined;
  }
  let env: any;
  try {
    env = (await res.json()).error;
  } catch {
    env = undefined;
  }
  const ra = Number(res.headers.get('Retry-After'));
  const err = new ApiError(res.status, env?.code ?? 'error', env?.message ?? res.statusText, {
    fields: env?.fields,
    retryAfter: ra > 0 ? ra : undefined,
    etag: res.headers.get('ETag') ?? undefined,
    body: env,
  });
  if (res.status === 401 && !o.noRedirect) handlers.unauthorized?.();
  else if (res.status >= 500) handlers.serverError?.(err);
  throw err;
}

export const api = {
  get<P extends Path<'get'>>(path: P, o?: Options<QueryOf<Op<P, 'get'>>>): Promise<ResOf<Op<P, 'get'>>> {
    return request('GET', path, o);
  },
  post<P extends Path<'post'>>(
    path: P,
    o?: Options<QueryOf<Op<P, 'post'>>, BodyOf<Op<P, 'post'>>>,
  ): Promise<ResOf<Op<P, 'post'>>> {
    return request('POST', path, o);
  },
  put<P extends Path<'put'>>(
    path: P,
    o?: Options<QueryOf<Op<P, 'put'>>, BodyOf<Op<P, 'put'>>>,
  ): Promise<ResOf<Op<P, 'put'>>> {
    return request('PUT', path, o);
  },
  patch<P extends Path<'patch'>>(
    path: P,
    o?: Options<QueryOf<Op<P, 'patch'>>, BodyOf<Op<P, 'patch'>>>,
  ): Promise<ResOf<Op<P, 'patch'>>> {
    return request('PATCH', path, o);
  },
  del<P extends Path<'delete'>>(
    path: P,
    o?: Options<QueryOf<Op<P, 'delete'>>, BodyOf<Op<P, 'delete'>>>,
  ): Promise<ResOf<Op<P, 'delete'>>> {
    return request('DELETE', path, o);
  },
};

export type Schemas = components['schemas'];
