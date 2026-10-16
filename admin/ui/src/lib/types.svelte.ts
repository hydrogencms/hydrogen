import { api } from './api/client';
import { session } from './auth/session.svelte';

export interface FieldShape {
  base: string;
  format?: string;
  enum?: string[];
  integer?: boolean;
  [k: string]: unknown;
}

export interface CompiledField {
  name: string;
  label: string;
  type: string;
  description?: string;
  required?: boolean;
  translatable?: boolean;
  index?: boolean;
  default?: unknown;
  when?: { field: string; op: string; value?: unknown };
  admin?: { widget?: string; placeholder?: string; hidden?: boolean; readonly?: boolean };
  shape: FieldShape;
  options?: Record<string, unknown>;
  of?: CompiledField;
  fields?: CompiledField[];
  group?: string;
}

export interface CompiledType {
  name: string;
  label: string;
  description?: string;
  singleton?: boolean;
  page?: boolean;
  /** Its blocks field can be embedded with a shared block. */
  blockset?: boolean;
  /** Publishing needs an approved review. */
  review?: boolean;
  namePattern?: string;
  admin?: { group?: string; icon?: string; list?: string[] };
  options?: Record<string, unknown>;
  fields: CompiledField[];
  /** Only on GET /types/{name}. */
  jsonSchema?: Record<string, unknown>;
}

export interface TypeGroup {
  /** admin.group; '' when ungrouped. */
  group: string;
  types: CompiledType[];
}

const byLabel = (a: CompiledType, b: CompiledType) => a.label.localeCompare(b.label) || a.name.localeCompare(b.name);

export function groupTypes(list: CompiledType[]): TypeGroup[] {
  const m = new Map<string, CompiledType[]>();
  for (const t of [...list].sort(byLabel)) {
    const g = t.admin?.group ?? '';
    m.set(g, [...(m.get(g) ?? []), t]);
  }
  return [...m.entries()]
    .sort(([a], [b]) => (a === '' ? -1 : b === '' ? 1 : a.localeCompare(b)))
    .map(([group, types]) => ({ group, types }));
}

class Types {
  all = $state.raw<CompiledType[]>([]);
  loaded = $state(false);
  loading = $state(false);
  error = $state<Error | null>(null);

  #details = new Map<string, CompiledType>();
  #inflight: Promise<void> | undefined;

  constructor() {
    session.onClear(() => this.reset());
  }

  load(force = false): Promise<void> {
    if (this.loaded && !force) return Promise.resolve();
    this.#inflight ??= (async () => {
      this.loading = true;
      this.error = null;
      try {
        this.all = (await api.get('/types')) as unknown as CompiledType[];
        this.loaded = true;
      } catch (e) {
        this.error = e as Error;
        throw e;
      } finally {
        this.loading = false;
        this.#inflight = undefined;
      }
    })();
    return this.#inflight;
  }

  reset() {
    this.all = [];
    this.loaded = false;
    this.error = null;
    this.#details.clear();
  }

  get(name: string): CompiledType | undefined {
    return this.all.find((t) => t.name === name);
  }

  /** The full compiled type with its JSON Schema attached (cached for the session). */
  async detail(name: string): Promise<CompiledType> {
    const hit = this.#details.get(name);
    if (hit) return hit;
    const r = (await api.get('/types/{type}', { params: { type: name } })) as unknown as { type: CompiledType; jsonSchema?: Record<string, unknown> };
    const t = { ...r.type, jsonSchema: r.jsonSchema };
    this.#details.set(name, t);
    return t;
  }

  /** Non-singleton, non-page types. */
  collections = $derived(this.all.filter((t) => !t.singleton && !t.page));
  pageTypes = $derived(this.all.filter((t) => t.page && !t.singleton));
  singletons = $derived(this.all.filter((t) => t.singleton));
  /** Every non-singleton type, page types included: what the Content navigation lists. */
  listable = $derived(this.all.filter((t) => !t.singleton));
}

export const types = new Types();
