export interface ApiErrorLike {
  status: number
  code: string
  message: string
  fields?: { path: string; code: string; message: string }[]
}

export interface AdminApi {
  get<T = any>(path: string, init?: RequestInit): Promise<T>
  post<T = any>(path: string, body?: unknown, init?: RequestInit): Promise<T>
  patch<T = any>(path: string, body?: unknown, init?: RequestInit): Promise<T>
  del<T = any>(path: string, init?: RequestInit): Promise<T>
}

export interface Principal {
  kind: string
  id?: string
  name: string
}

export interface AdminContext {
  api: AdminApi
  t(key: string, vars?: Record<string, unknown>): string
  navigate(path: string): void
  toast(kind: 'info' | 'success' | 'error', text: string): void
  principal: Principal
  can(policy: string, opts?: { type?: string }): boolean
  locale: string
  prefix: string
}

export type SlotPosition =
  | 'login.before' | 'login.after' | 'list.before' | 'list.after'
  | 'editor.before' | 'editor.after' | 'dashboard.before' | 'dashboard.after'

export interface ExtensionManifest {
  elements?: string[]
  theme?: { name?: string; logo?: string; accent?: string; css?: string }
  strings?: string[]
  widgets?: Record<string, string>
  cells?: Record<string, string>
  panels?: { tag: string; title: string; types?: string[]; policy?: string }[]
  actions?: { tag: string; title: string; types?: string[]; policy?: string }[]
  dashboard?: { tag: string; title?: string; policy?: string }[]
  views?: { path: string; tag: string; menu?: string; title: string; policy?: string }[]
  slots?: { tag: string; at: SlotPosition; policy?: string }[]
}

export type CompiledField = { name: string; type: string; [key: string]: unknown }
export type ItemSummary = { id: string; type?: string; [key: string]: unknown }
export type ItemView = { id: string; type?: string; fields?: Record<string, unknown>; [key: string]: unknown }

interface WithCtx { ctx: AdminContext }

export interface WidgetProps extends WithCtx {
  value: unknown
  field: CompiledField
  locale: string
  readonly: boolean
  error?: string
}
export type WidgetChangeEvent = CustomEvent<{ value: unknown }>

export interface CellProps extends WithCtx { value: unknown; item: ItemSummary; field: CompiledField }

export interface PanelProps extends WithCtx { item: ItemView; type: string; locale: string; draft: boolean }
export type PanelRefreshEvent = CustomEvent<undefined>

export interface ActionProps extends WithCtx { item: ItemView; type: string; locale: string }
export type ActionRefreshEvent = CustomEvent<undefined>
export type NavigateEvent = CustomEvent<{ path: string }>

export type DashboardProps = WithCtx

export interface ViewProps extends WithCtx { params: Record<string, string>; query: Record<string, string> }

export interface SlotProps extends WithCtx { at: SlotPosition; item?: ItemView; type?: string }

/** Registers a custom element once; a tag that is already defined is skipped with one warning (HMR). */
export function defineElement(tag: string, ctor: CustomElementConstructor): void {
  const reg = globalThis.customElements
  if (reg.get(tag)) {
    if (!warned.has(tag)) {
      warned.add(tag)
      console.warn(`${tag}: already defined, skipping redefinition (reload the page to pick up changes)`)
    }
    return
  }
  reg.define(tag, ctor)
}
const warned = new Set<string>()

/** Dispatches a bubbling, composed CustomEvent from el. */
export function emit<T = unknown>(el: EventTarget, name: string, detail?: T): boolean {
  return el.dispatchEvent(new CustomEvent(name, { detail, bubbles: true, composed: true }))
}

/** Reads the ctx property the admin sets before connecting the element. */
export function useContext(el: Element | { ctx?: AdminContext; localName?: string }): AdminContext {
  const ctx = (el as { ctx?: AdminContext }).ctx
  if (!ctx) throw new Error(`${(el as { localName?: string }).localName || 'element'}: missing ctx; was it mounted by the admin?`)
  return ctx
}
