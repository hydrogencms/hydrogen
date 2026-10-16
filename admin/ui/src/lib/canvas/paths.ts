// Frame paths ("fields/body/2/slots/cards/0/props/heading") <-> block trees and form paths. Pure.
import { blockPath, propsPath, slotPath, type Block, type Container } from '../blocks/tree';

/** Index, slot name, index ...: [2, 'cards', 0]. */
export type Step = number | string;
export interface FramePath {
  field: string;
  chain: Step[];
  prop?: string;
}

const INT = /^(0|[1-9]\d*)$/;

/** Splits `fields/<name>/<i>(/slots/<s>/<i>)*` and returns the steps; a trailing slot name is allowed when `slot`. */
function steps(segs: string[], slot: boolean): Step[] | null {
  const chain: Step[] = [];
  let i = 0;
  while (i < segs.length) {
    if (!INT.test(segs[i]!)) return null;
    chain.push(Number(segs[i]));
    i++;
    if (i === segs.length) break;
    if (segs[i] !== 'slots' || i + 1 >= segs.length || !segs[i + 1]) return null;
    chain.push(segs[i + 1]!);
    i += 2;
    if (i === segs.length) return slot ? chain : null;
  }
  return chain;
}

export function parsePath(p: string): FramePath | null {
  const segs = p.split('/');
  if (segs[0] !== 'fields' || segs.length < 2 || !segs[1]) return null;
  const field = segs[1];
  let rest = segs.slice(2);
  let prop: string | undefined;
  // `props` is a keyword only right after a block index (a slot may be named "props").
  const at = rest.length - 2;
  if (at > 0 && rest[at] === 'props' && INT.test(rest[at - 1]!) && rest[at + 1]) {
    prop = rest[at + 1];
    rest = rest.slice(0, at);
  }
  const chain = steps(rest, false);
  if (!chain) return null;
  return prop === undefined ? { field, chain } : { field, chain, prop };
}

export function formatPath(fp: FramePath): string {
  let s = `fields/${fp.field}`;
  for (const st of fp.chain) s += typeof st === 'number' ? `/${st}` : `/slots/${st}`;
  return fp.prop === undefined ? s : `${s}/props/${fp.prop}`;
}

export function locate(root: Block[], chain: Step[]): { block: Block; container: Container; index: number } | undefined {
  if (!chain.length || chain.length % 2 === 0) return undefined;
  let list = root;
  let container: Container = {};
  for (let i = 0; i < chain.length; i += 2) {
    const idx = chain[i];
    if (typeof idx !== 'number' || !Number.isInteger(idx) || idx < 0) return undefined;
    const block = list[idx];
    if (!block) return undefined;
    if (i === chain.length - 1) return { block, container, index: idx };
    const slot = chain[i + 1];
    if (typeof slot !== 'string') return undefined;
    container = { id: block.id, slot };
    list = block.slots?.[slot] ?? [];
  }
  return undefined;
}

export function chainOf(root: Block[], id: string): Step[] | undefined {
  for (let i = 0; i < root.length; i++) {
    const b = root[i]!;
    if (b.id === id) return [i];
    for (const [slot, list] of Object.entries(b.slots ?? {})) {
      const sub = chainOf(list, id);
      if (sub) return [i, slot, ...sub];
    }
  }
  return undefined;
}

export function blockPathOf(field: string, root: Block[], id: string): string | undefined {
  const chain = chainOf(root, id);
  return chain && formatPath({ field, chain });
}

/** The form path (tree.ts conventions): 'body[2].slots.cards[0].props.heading', or 'title'. */
export function formPath(fp: FramePath): string {
  let s = fp.field;
  for (let i = 0; i < fp.chain.length; i += 2) {
    s = blockPath(s, fp.chain[i] as number);
    if (i + 1 < fp.chain.length) s = slotPath(s, fp.chain[i + 1] as string);
  }
  if (fp.prop === undefined) return s;
  return fp.chain.length ? `${propsPath(s)}.${fp.prop}` : s;
}

export function listPath(p: string): { list: string; index: number } | undefined {
  const at = p.lastIndexOf('/');
  if (at < 0 || !INT.test(p.slice(at + 1))) return undefined;
  const list = p.slice(0, at);
  return parseList(list) ? { list, index: Number(p.slice(at + 1)) } : undefined;
}

function parseList(list: string): { field: string; chain: Step[] } | null {
  const segs = list.split('/');
  if (segs[0] !== 'fields' || segs.length < 2 || !segs[1]) return null;
  const chain = steps(segs.slice(2), true);
  if (!chain) return null;
  // A list is the field itself (no steps) or ends in a slot name.
  if (chain.length && chain.length % 2 === 1) return null;
  return { field: segs[1], chain };
}

export function containerOf(root: Block[], list: string): { field: string; container: Container } | undefined {
  const p = parseList(list);
  if (!p) return undefined;
  if (!p.chain.length) return { field: p.field, container: {} };
  const slot = p.chain[p.chain.length - 1] as string;
  const owner = locate(root, p.chain.slice(0, -1));
  if (!owner?.block.id) return undefined;
  return { field: p.field, container: { id: owner.block.id, slot } };
}

export function isBlockPath(p: string): boolean {
  const fp = parsePath(p);
  return !!fp && fp.prop === undefined && fp.chain.length > 0;
}
