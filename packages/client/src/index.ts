export interface RequestMeta {
  url: string
  path: string
  headers: Record<string, string>
}

export interface ClientOptions {
  /** e.g. "https://cms.acme.com"; the delivery API is at /api/deliver/v1 */
  baseUrl: string
  /** public channel name; omit for token channels or single-channel sites */
  channel?: string
  /** channel token (Authorization: Bearer) */
  token?: string
  /** preview token (X-Hydrogen-Preview) */
  preview?: string
  fetch?: typeof fetch
  headers?: Record<string, string>
  /**
   * Per-client hook for adapters: extra RequestInit for every request (cache options of a framework's fetch).
   * `GetOptions.init` is the per-call counterpart for application code; per-call values win.
   */
  init?: (req: RequestMeta) => RequestInit | undefined
}

export interface GetOptions {
  locale?: string
  /** resolve referenced items to this depth (0..3) into `included` */
  include?: number
  /** field paths to return, dot paths into groups (`['title', 'seo.title']`); the item's other fields are left out, and include/links follow only the selected fields */
  fields?: string | string[]
  /** overrides the client's preview token for this call */
  preview?: string
  /** extra request headers (e.g. the visitor's headers that header matchers look at) */
  headers?: Record<string, string>
  signal?: AbortSignal
  init?: RequestInit
}

export type Op = 'eq' | 'ne' | 'lt' | 'lte' | 'gt' | 'gte' | 'in' | 'contains' | 'exists' | 'prefix'
type Scalar = string | number | boolean
export type Condition = Scalar | { [K in Op]?: K extends 'in' ? Scalar[] | string : K extends 'exists' ? boolean : Scalar }
export type Where<F> = { [K in keyof F & string]?: Condition } & Record<string, Condition | undefined>

export interface ListOptions<F = Record<string, unknown>> extends GetOptions {
  where?: Where<F>
  /** any of these groups: an item matches when it satisfies every condition of at least one (`where[or][i]…`) */
  or?: Where<F>[]
  /** none of these groups: an item is left out when it satisfies every condition of any of them (`where[not][i]…`); items without a value for a field pass a test on it */
  not?: Where<F>[]
  /** indexed text fields (at most 5) to count over all matches of the filters, answered as `facets` */
  facets?: string | string[]
  /** "-publishedAt,title", or fields in order; a leading "-" sorts descending */
  sort?: string | string[]
  limit?: number
  offset?: number
  q?: string
  /** ask for the exact `total` (sent as total=true only when true) */
  total?: boolean
}

export interface Item<F> {
  id: string
  type: string
  key?: string
  locale: string
  version: number
  publishedAt: string
  name: string
  fields: F
  fallback?: boolean
  url?: string
  links?: Record<string, string>
}

export interface List<F> {
  items: Item<F>[]
  hasMore: boolean
  total?: number
  /** value counts per requested facet field, most items first */
  facets?: Record<string, { value: string; count: number }[]>
  included?: Record<string, Item<unknown>>
}

export interface Response<D> {
  data: D
  /** cache tags from Surrogate-Key */
  tags: string[]
  etag?: string
  status: number
  /** referenced items of single-item calls, by id (when `include` was given) */
  included?: Record<string, Item<unknown>>
}

export interface ChannelOptions {
  site?: string
  match?: { host?: string; prefix?: string; header?: { name: string; value: string } }
  reserved?: string[]
  [k: string]: unknown
}

export interface ChannelInfo {
  name: string
  languages: string[]
  collections: string[]
  /** the frontend this channel serves (origin, maybe a base path), when the channel has one */
  frontend?: string
  options: ChannelOptions
}

export interface Crumb { node: string; item?: string; name: string; path: string }
export interface Resolution {
  kind: string
  item?: string
  node?: string
  path?: string
  status: number
  location?: string
  breadcrumbs?: Crumb[]
}
export interface Alternate { channel: string; locale: string; url: string }
export type LocatedKind = 'page' | 'item' | 'redirect' | 'not_found' | 'reserved' | 'no_channel'
export interface Located {
  channel?: string
  kind: LocatedKind
  locale?: string
  resolution?: Resolution
  canonical?: string
  alternates?: Alternate[]
}
export interface RouteResult { located: Located; item?: Item<unknown> }

export interface TreeNode {
  id: string
  kind: 'page' | 'mount' | 'link'
  item?: string
  type?: string
  name: string
  path?: string
  url?: string
  /** the selected fields of the page's item (when `fields` was asked) */
  fields?: Record<string, unknown>
  nodes?: TreeNode[]
}

export interface FieldError { path: string; code: string; message: string }

export class HydrogenError extends Error {
  status: number
  code: string
  fields?: FieldError[]
  constructor(status: number, code: string, message: string, fields?: FieldError[]) {
    super(message)
    this.name = 'HydrogenError'
    this.status = status
    this.code = code
    this.fields = fields
  }
}

export interface Client<T> {
  item<K extends keyof T & string>(id: string, o?: GetOptions): Promise<Response<Item<T[K]>>>
  itemByKey(key: string, o?: GetOptions): Promise<Response<Item<unknown>>>
  singleton<K extends keyof T & string>(type: K, o?: GetOptions): Promise<Response<Item<T[K]>>>
  list<K extends keyof T & string>(type: K, o?: ListOptions<T[K]>): Promise<Response<List<T[K]>>>
  route(url: string, o?: GetOptions): Promise<Response<RouteResult>>
  /** the nodes of /tree; site and locale of the answer are not part of `data` */
  tree(o?: { site?: string; locale?: string; depth?: number } & GetOptions): Promise<Response<TreeNode[]>>
  channel(name?: string, o?: GetOptions): Promise<Response<ChannelInfo>>
  withPreview(token: string | undefined): Client<T>
}

export const API_PATH = '/api/deliver/v1'

const enc = encodeURIComponent
const fieldList = (f?: string | string[]) => (Array.isArray(f) ? f.join(',') : f)

/** Public so adapters and tests can build delivery URLs the same way; applications use the client. Builds a query string: parameters ordered by encoded name (where[...] included), values percent-encoded, empty values dropped. */
export function buildQuery(
  p: Record<string, string | number | undefined>,
  where?: Record<string, Condition | undefined>,
  or?: Record<string, Condition | undefined>[],
  not?: Record<string, Condition | undefined>[],
): string {
  const out: [string, string][] = []
  const add = (k: string, v: string) => out.push([k.replace(/[^\w[\]-]+/g, enc), enc(v)])
  for (const k in p) {
    const v = p[k]
    if (v !== undefined && v !== '') add(k, String(v))
  }
  const conds = (pre: string, w?: Record<string, Condition | undefined>) => {
    for (const f in w) {
      const c = w[f]
      if (c === undefined) continue
      if (typeof c === 'object') {
        for (const op in c) {
          const v = (c as Record<string, Scalar | Scalar[] | undefined>)[op]
          if (v !== undefined) add(`${pre}[${f}][${op}]`, Array.isArray(v) ? v.join(',') : String(v))
        }
      } else add(`${pre}[${f}][eq]`, String(c))
    }
  }
  conds('where', where)
  or?.forEach((g, i) => conds(`where[or][${i}]`, g))
  not?.forEach((g, i) => conds(`where[not][${i}]`, g))
  out.sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0))
  return out.map(([k, v]) => `${k}=${v}`).join('&')
}

export function parseTags(h: string | null | undefined): string[] {
  return h ? h.split(/\s+/).filter(Boolean) : []
}

export function linkTo(
  item: { links?: Record<string, string> },
  id: string,
  fallback?: (id: string) => string | undefined,
): string | undefined {
  return item.links?.[id] ?? fallback?.(id)
}

export interface ImageVariant { url: string; width: number; height: number; type: string; srcset?: string }

/** The part of a delivery asset that `imageProps` reads; the generated `Asset` fits. */
export interface ImageAsset {
  url: string
  width?: number
  height?: number
  focal?: { x: number; y: number }
  variants?: Record<string, ImageVariant>
  fields?: unknown
}

export interface ImageProps { src: string; srcSet?: string; width?: number; height?: number; alt: string }

/** `<img>` attributes for an asset: the preset's variant when it has one, else the original file. */
export function imageProps(asset: ImageAsset, preset?: string): ImageProps {
  const v = preset ? asset.variants?.[preset] : undefined
  const alt = (asset.fields as { alt?: unknown } | null | undefined)?.alt
  return {
    src: v ? v.url : asset.url,
    srcSet: v?.srcset,
    width: v ? v.width : asset.width,
    height: v ? v.height : asset.height,
    alt: typeof alt === 'string' ? alt : '',
  }
}

export function createClient<T extends object = Record<string, unknown>>(o: ClientOptions): Client<T> {
  const base = o.baseUrl.replace(/\/+$/, '') + API_PATH
  const doFetch = (u: string, i: RequestInit) => (o.fetch ?? fetch)(u, i)

  async function get<D>(path: string, query: string, g: GetOptions = {}, pick?: (b: any) => D): Promise<Response<D>> {
    const headers: Record<string, string> = { Accept: 'application/json', ...o.headers, ...g.headers }
    if (o.token) headers.Authorization = `Bearer ${o.token}`
    const pv = g.preview ?? o.preview
    if (pv) headers['X-Hydrogen-Preview'] = pv
    const url = base + path + (query ? '?' + query : '')
    const extra = o.init?.({ url, path, headers })
    const res = await doFetch(url, { ...extra, ...g.init, method: 'GET', headers: { ...extra?.headers as Record<string, string>, ...headers }, signal: g.signal ?? g.init?.signal ?? extra?.signal })
    let body: any
    const text = await res.text()
    try { body = text ? JSON.parse(text) : undefined } catch { body = undefined }
    if (!res.ok) {
      const e = body?.error
      throw new HydrogenError(res.status, e?.code ?? (res.status === 404 ? 'not_found' : 'error'), e?.message ?? res.statusText ?? 'request failed', e?.fields)
    }
    const r: Response<D> = {
      data: pick ? pick(body) : body,
      tags: parseTags(res.headers.get('Surrogate-Key')),
      etag: res.headers.get('ETag') ?? undefined,
      status: res.status,
    }
    return r
  }

  const common = (g?: GetOptions) => ({ channel: o.channel, locale: g?.locale, include: g?.include, fields: fieldList(g?.fields) })

  async function one<D>(path: string, g?: GetOptions): Promise<Response<Item<D>>> {
    const r = await get<any>(path, buildQuery(common(g)), g)
    const res: Response<Item<D>> = { ...r, data: r.data.item }
    if (r.data.included) res.included = r.data.included
    return res
  }

  const c: Client<T> = {
    item: (id, g) => one(`/items/${enc(id)}`, g),
    itemByKey: (key, g) => one(`/items/by-key/${enc(key)}`, g),
    singleton: (type, g) => one(`/singletons/${enc(type)}`, g),
    list: (type, l) =>
      get(`/collections/${enc(type)}/items`, buildQuery({
        ...common(l),
        q: l?.q,
        limit: l?.limit,
        offset: l?.offset,
        total: l?.total ? 'true' : undefined,
        sort: Array.isArray(l?.sort) ? l.sort.join(',') : l?.sort,
        facets: fieldList(l?.facets),
      }, l?.where as Record<string, Condition | undefined>, l?.or, l?.not), l),
    route: (url, g) => get('/route', buildQuery({ channel: o.channel, locale: g?.locale, url }), g),
    tree: (t) => get('/tree', buildQuery({ channel: o.channel, locale: t?.locale, site: t?.site, depth: t?.depth, fields: fieldList(t?.fields) }), t, (b) => b.nodes ?? []),
    channel: (name, g) => {
      const n = name ?? o.channel
      if (!n) return Promise.reject(new HydrogenError(400, 'bad_request', 'channel name is required'))
      return get(`/channels/${enc(n)}`, '', g)
    },
    withPreview: (token) => createClient<T>({ ...o, preview: token }),
  }
  return c
}
