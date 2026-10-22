// Block-level plain-text diff of stored rich text documents (`{type: 'doc', content: [...]}`). Pure.
import { diffSeq, diffWordsEx, hasChange, type Seg } from './seq';

interface Node {
  type?: string;
  text?: string;
  attrs?: Record<string, unknown>;
  content?: Node[];
}

export interface RichRow {
  status: 'added' | 'removed' | 'changed';
  /** The node kind of the new side (the old side when removed): `paragraph`, `heading 2`, `bulletList`... */
  node: string;
  /** The old kind when it differs from `node`. */
  from?: string;
  segs: Seg[];
  /** The text was too long to match word by word. */
  approx?: boolean;
  /** Only the formatting changed (marks, link targets): the text is identical. */
  formatting?: boolean;
}

export interface RichDiff {
  rows: RichRow[];
  /** Blocks present and identical on both sides (collapsed). */
  same: number;
}

const INLINE = new Set(['text', 'hardBreak', 'hard_break']);
// Built-in nodes that can be empty without being leaves.
const PLAIN = new Set(['paragraph', 'heading', 'bulletList', 'orderedList', 'listItem', 'blockquote', 'codeBlock', 'table', 'tableRow', 'tableCell', 'tableHeader', 'horizontalRule', 'doc']);

export function textOf(n: Node | undefined): string {
  if (!n) return '';
  if (n.type === 'text') return n.text ?? '';
  if (n.type === 'hardBreak' || n.type === 'hard_break') return '\n';
  if (n.type === 'image') return `[image: ${[n.attrs?.alt, n.attrs?.src].filter(Boolean).join(' ')}]`;
  // A custom leaf (embed, footnote reference) has no text: show its type and attribute values.
  if (!n.content && n.type && !PLAIN.has(n.type)) return `[${[n.type, ...Object.values(n.attrs ?? {})].join(' ')}]`;
  const kids = n.content ?? [];
  const parts = kids.map(textOf);
  if (n.type === 'tableRow') return parts.join(' | ');
  return kids.every((k) => INLINE.has(k.type ?? '')) ? parts.join('') : parts.join('\n');
}

const kindOf = (n: Node): string => (n.attrs?.level ? `${n.type} ${n.attrs.level}` : (n.type ?? ''));

/** Top-level nodes of a document; a bare string becomes one paragraph. */
export function blocksOf(v: unknown): Node[] {
  if (typeof v === 'string') return v ? [{ type: 'paragraph', content: [{ type: 'text', text: v }] }] : [];
  const doc = v as Node | null | undefined;
  return Array.isArray(doc?.content) ? doc!.content! : [];
}

export const isEmptyRich = (v: unknown): boolean => blocksOf(v).every((n) => !textOf(n) && !(n.content ?? []).length);

const key = (n: Node): string => JSON.stringify(n);

export function diffRich(a: unknown, b: unknown): RichDiff {
  const A = blocksOf(a);
  const B = blocksOf(b);
  const ops = diffSeq(A.map(key), B.map(key));
  const rows: RichRow[] = [];
  let same = 0;
  let dels: Node[] = [];
  let inss: Node[] = [];
  const flush = () => {
    const n = Math.max(dels.length, inss.length);
    // Pair deletions with insertions in order: an edited block is one row with a word diff.
    for (let i = 0; i < n; i++) {
      const d = dels[i];
      const s = inss[i];
      if (d && s) {
        const { segs, approx } = diffWordsEx(textOf(d), textOf(s));
        const fromKind = kindOf(d);
        rows.push({
          status: 'changed',
          node: kindOf(s),
          from: fromKind !== kindOf(s) ? fromKind : undefined,
          segs,
          approx: approx || undefined,
          formatting: !hasChange(segs) || undefined,
        });
      } else if (s) rows.push({ status: 'added', node: kindOf(s), segs: diffWordsEx(undefined, textOf(s)).segs });
      else if (d) rows.push({ status: 'removed', node: kindOf(d), segs: diffWordsEx(textOf(d), undefined).segs });
    }
    dels = [];
    inss = [];
  };
  for (const op of ops) {
    if (op.k === 'eq') {
      flush();
      same++;
    } else if (op.k === 'del') dels.push(A[op.a!]!);
    else inss.push(B[op.b!]!);
  }
  flush();
  return { rows, same };
}
