// Cleaning and hydrating stored block values. Kept apart from tree.ts: fields/logic.ts imports it eagerly.
export interface RawBlock {
  id?: string;
  type: string;
  props?: Record<string, unknown>;
  slots?: Record<string, RawBlock[]>;
  [k: string]: unknown;
}

const empty = (v: unknown) => v === undefined || v === null || v === '' || (typeof v === 'number' && Number.isNaN(v));

/** The tree as it is sent: empty props dropped (the server treats them as absent), empty slots omitted. */
export function cleanBlocks(list: unknown): unknown {
  if (!Array.isArray(list)) return list;
  return list.map((raw) => {
    if (!raw || typeof raw !== 'object') return raw;
    const b = raw as RawBlock;
    const out: RawBlock = { ...b, props: Object.fromEntries(Object.entries(b.props ?? {}).filter(([, v]) => !empty(v))) };
    if (b.slots) {
      const slots: Record<string, RawBlock[]> = {};
      for (const [k, v] of Object.entries(b.slots)) if (Array.isArray(v) && v.length) slots[k] = cleanBlocks(v) as RawBlock[];
      if (Object.keys(slots).length) out.slots = slots;
      else delete out.slots;
    }
    return out;
  });
}

/** Loaded blocks always carry a `props` object, so the property widgets have something to write into. */
export function hydrateBlocks(list: unknown): unknown {
  if (!Array.isArray(list)) return list;
  return list.map((raw) => {
    if (!raw || typeof raw !== 'object') return raw;
    const b = raw as RawBlock;
    const out: RawBlock = { ...b, props: b.props ?? {} };
    if (b.slots) out.slots = Object.fromEntries(Object.entries(b.slots).map(([k, v]) => [k, hydrateBlocks(v) as RawBlock[]]));
    return out;
  });
}

