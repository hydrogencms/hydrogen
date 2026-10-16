// Plain-text values the frame may map to elements (overlay.texts) and which of them the canvas can edit inline.
import type { CompiledField } from '../types.svelte';
import { SHARED, type Block, type BlockType } from '../blocks/tree';
import { formatPath, locate, parsePath } from './paths';

export interface TextRef {
  path: string;
  text: string;
  scope: string;
}

const MAX = 500;
type Lookup = (name: string) => BlockType | undefined;

const isText = (f: CompiledField) => f.type === 'string' || f.type === 'text';
const hidden = (f: CompiledField) => !!f.admin?.hidden;
const blocksField = (f: CompiledField) => f.type === 'blocks' || f.shape?.base === 'blocks';
const ok = (v: unknown): v is string => typeof v === 'string' && v !== '' && v.length <= MAX;

export function textRefs(fields: CompiledField[], docFields: Record<string, unknown>, blockType: Lookup): TextRef[] {
  const out: TextRef[] = [];
  const walk = (field: string, list: Block[], chain: (number | string)[]) => {
    list.forEach((b, i) => {
      if (!b || typeof b !== 'object' || b.type === SHARED) return;
      const c = [...chain, i];
      const bp = formatPath({ field, chain: c });
      for (const p of blockType(b.type)?.props ?? []) {
        const v = b.props?.[p.name];
        if (isText(p) && !hidden(p) && ok(v)) out.push({ path: `${bp}/props/${p.name}`, text: v, scope: bp });
      }
      for (const [slot, sub] of Object.entries(b.slots ?? {})) if (Array.isArray(sub)) walk(field, sub, [...c, slot]);
    });
  };
  for (const f of fields) {
    if (hidden(f)) continue;
    const v = docFields[f.name];
    if (isText(f)) {
      if (ok(v)) out.push({ path: `fields/${f.name}`, text: v, scope: '' });
    } else if (blocksField(f) && Array.isArray(v)) walk(f.name, v as Block[], []);
  }
  return out.sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
}

export function editableText(
  fields: CompiledField[],
  blockType: Lookup,
  docFields: Record<string, unknown>,
  path: string,
): { multiline: boolean; label: string } | null {
  const fp = parsePath(path);
  if (!fp) return null;
  const top = fields.find((f) => f.name === fp.field);
  if (!top || hidden(top) || top.admin?.readonly) return null;
  let f: CompiledField | undefined;
  if (!fp.chain.length) {
    if (fp.prop !== undefined) return null;
    f = top;
  } else {
    if (fp.prop === undefined || !blocksField(top)) return null;
    const v = docFields[fp.field];
    const hit = Array.isArray(v) ? locate(v as Block[], fp.chain) : undefined;
    if (!hit || hit.block.type === SHARED) return null;
    f = blockType(hit.block.type)?.props.find((p) => p.name === fp.prop);
  }
  if (!f || !isText(f) || hidden(f) || f.admin?.readonly) return null;
  return { multiline: f.type === 'text', label: f.label };
}
