import { Schema, type DOMOutputSpec, type MarkSpec, type Node as PMNode, type NodeSpec } from 'prosemirror-model';

// The ProseMirror schema of a rich text field. Node and mark names and attributes are exactly what the server
// validates (schema/richtext.go), built-in and declared (`richtext:` in the project schema). Everything else is a
// pure editor concern.

export { configOf, type RichConfig } from './config';
import type { RichAttrDef, RichConfig, RichCustom, RichMarkDef, RichNodeDef } from './config';

/** Anchor ids (paragraph and heading `id`, `#id` links). */
export const ID_RE = /^[A-Za-z][A-Za-z0-9_-]{0,63}$/;
export const ALIGNS = ['left', 'center', 'right', 'justify'] as const;
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/** href rules of the server: http(s) URL, mailto: address, item:<uuid>[#id] or #id. */
export function validHref(h: string): boolean {
  if (h.startsWith('#')) return ID_RE.test(h.slice(1));
  if (h.startsWith('item:')) {
    const [id, frag, ...rest] = h.slice(5).split('#');
    return UUID_RE.test(id) && rest.length === 0 && (frag === undefined || ID_RE.test(frag));
  }
  if (h.startsWith('mailto:')) return h.length > 7;
  try {
    const u = new URL(h);
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.host !== '';
  } catch {
    return false;
  }
}

/** What an editor typed becomes a stored href: bare domains get https://, bare addresses mailto:. */
export function normalizeHref(raw: string): string {
  const v = raw.trim();
  if (!v || v.startsWith('#') || /^[a-z][a-z0-9+.-]*:/i.test(v)) return v;
  if (/^[^\s@/]+@[^\s@/]+\.[^\s@/]+$/.test(v)) return `mailto:${v}`;
  return /^[^\s/]+\.[^\s/]+/.test(v) ? `https://${v}` : v;
}

/** src rules of the server: http(s) URL or root-relative path (not protocol-relative); no backslashes or control characters. */
export function validImageSrc(s: string): boolean {
  // eslint-disable-next-line no-control-regex
  if (!s || /[\\\u0000-\u001f\u007f]/.test(s)) return false;
  if (s.startsWith('/')) return !s.startsWith('//');
  try {
    const u = new URL(s);
    return (u.protocol === 'http:' || u.protocol === 'https:') && u.host !== '';
  } catch {
    return false;
  }
}

const headingTag = (level: number) => `h${Math.min(6, Math.max(1, level))}` as 'h1';

const kebab = (s: string) => s.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`);

/** An attribute value as the declaration allows it. */
export function attrOk(a: RichAttrDef, v: unknown): boolean {
  switch (a.type) {
    case 'string':
      return typeof v === 'string' && v.length <= (a.maxLength ?? 500) && !v.includes('\u0000');
    case 'enum':
      return typeof v === 'string' && !!a.values?.includes(v);
    case 'int':
      return typeof v === 'number' && Number.isInteger(v) && v >= (a.min ?? -1e9) && v <= (a.max ?? 1e9);
    case 'bool':
      return typeof v === 'boolean';
    case 'url':
      return typeof v === 'string' && validImageSrc(v);
  }
}

/** The data-* attributes of a custom node or mark: the default rendering every frontend shares. */
function dataAttrs(defs: RichAttrDef[], attrs: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const a of defs) {
    const v = attrs[a.name] ?? a.default;
    if (v === null || v === undefined || v === false) continue;
    out[`data-${kebab(a.name)}`] = v === true ? '' : String(v);
  }
  return out;
}

/** Reads the attributes back from `data-*`; false when a required one is missing or invalid. */
function readAttrs(defs: RichAttrDef[], d: HTMLElement): Record<string, unknown> | false {
  const out: Record<string, unknown> = {};
  for (const a of defs) {
    const raw = d.getAttribute(`data-${kebab(a.name)}`);
    let v: unknown = null;
    if (a.type === 'bool') v = raw !== null ? true : a.default === true ? false : null;
    else if (raw !== null) {
      v = a.type === 'int' ? (raw.trim() === '' ? NaN : Number(raw)) : raw;
      if (!attrOk(a, v)) v = null;
    }
    if (v === null && a.required) return false;
    out[a.name] = v;
  }
  return out;
}

const attrSpecs = (defs: RichAttrDef[]) => Object.fromEntries(defs.map((a) => [a.name, { default: null }]));

/** What a leaf shows inside the editor, where the empty element of the default rendering would be invisible. */
function leafLabel(label: string, defs: RichAttrDef[], attrs: Record<string, unknown>): string {
  const shown = defs.map((a) => attrs[a.name]).find((v) => typeof v === 'string' || typeof v === 'number');
  return shown === undefined ? label : `${label}: ${shown}`;
}

function customNode(d: RichNodeDef, any: string, inline: string): NodeSpec {
  const leaf = d.content === 'none';
  const spec: NodeSpec = {
    attrs: attrSpecs(d.attrs),
    parseDOM: [{ tag: `${d.tag}.rt-${d.name}`, getAttrs: (el) => readAttrs(d.attrs, el) }],
    toDOM: (n) => {
      const a = { class: `rt-${d.name}`, ...dataAttrs(d.attrs, n.attrs) };
      return leaf ? [d.tag, a, ['span', { class: 'h-rte__leaf' }, leafLabel(d.label || d.name, d.attrs, n.attrs)]] : [d.tag, a, 0];
    },
  };
  if (d.content === 'blocks') spec.content = `(${any})+`;
  else if (d.content === 'inline') spec.content = `(${inline})*`;
  else if (d.content === 'parts') spec.content = d.parts.join(' ');
  if (d.inline) {
    spec.inline = true;
    spec.group = 'inline';
  } else if (!d.part) {
    spec.group = 'block';
  }
  if (d.part) spec.isolating = true;
  else if (!leaf && !d.inline) spec.defining = true;
  if (leaf && !d.inline) spec.draggable = true;
  return spec;
}

function customMark(d: RichMarkDef): MarkSpec {
  return {
    attrs: attrSpecs(d.attrs),
    parseDOM: [{ tag: `${d.tag}.rt-${d.name}`, getAttrs: (el) => readAttrs(d.attrs, el) }],
    toDOM: (m) => [d.tag, { class: `rt-${d.name}`, ...dataAttrs(d.attrs, m.attrs) }, 0],
  };
}

const INLINE_TAGS = new Set('a abbr b bdi bdo br cite code data dfn del em i ins kbd mark q s samp small span strike strong sub sup time u var'.split(' '));
const alignOf = (d: HTMLElement) => {
  const v = d.getAttribute('data-align') || d.style.textAlign;
  return v === 'center' || v === 'right' || v === 'justify' ? v : null;
};
const anchorOf = (d: HTMLElement) => {
  const v = d.getAttribute('data-anchor') || d.id;
  return ID_RE.test(v) ? v : null;
};
const blockAttrs = (d: HTMLElement) => ({ align: alignOf(d), id: anchorOf(d) });
const blockDOM = (n: PMNode) => ({ ...(n.attrs.align ? { 'data-align': n.attrs.align as string } : {}), ...(n.attrs.id ? { 'data-anchor': n.attrs.id as string } : {}) });
const tightOf = (d: HTMLElement) => {
  if (d.hasAttribute('data-tight')) return true;
  const items = [...d.children].filter((c) => c.tagName === 'LI');
  return items.length > 0 && items.every((li) => [...li.children].every((c) => INLINE_TAGS.has(c.tagName.toLowerCase()))) ? true : null;
};
const startOf = (d: HTMLElement) => {
  const raw = d.getAttribute('start');
  const v = raw === null || raw.trim() === '' ? NaN : Number(raw);
  return Number.isInteger(v) && v !== 1 && Math.abs(v) <= 1e6 ? v : null;
};
const listDOM = (tag: 'ul' | 'ol', n: PMNode): DOMOutputSpec => {
  const a: Record<string, string> = {};
  if (n.attrs.tight) a['data-tight'] = 'true';
  if (tag === 'ol' && n.attrs.start !== null && n.attrs.start !== 1) a.start = String(n.attrs.start);
  return [tag, a, 0];
};

export function buildSchema(cfg: RichConfig = {}, custom?: RichCustom): Schema {
  const defs = new Map<string, RichNodeDef>((custom?.nodes ?? []).map((d) => [d.name, d]));
  const parentOf = new Map<string, string>();
  for (const d of defs.values()) for (const p of d.parts) parentOf.set(p, d.name);
  // A part node follows its parent; the parent chain has no cycles (the server rejects them).
  const allowed = (n: string, depth = 0): boolean => {
    if (defs.get(n)?.part) {
      const p = parentOf.get(n);
      return !!p && depth < 20 && allowed(p, depth + 1);
    }
    return !cfg.nodes || cfg.nodes.includes(n);
  };
  const node = (n: string) => allowed(n);
  const mark = (m: string) => !cfg.marks || cfg.marks.includes(m);
  const lists = node('bulletList') || node('orderedList');
  const customNodes = [...defs.values()].filter((d) => allowed(d.name));

  const text = ['paragraph', 'heading'].filter(node);
  if (text.length === 0) throw new Error('a rich text field must allow paragraph or heading nodes');
  const blocks = [
    ...text,
    ...['bulletList', 'orderedList', 'blockquote', 'codeBlock', 'horizontalRule', 'image', 'table'].filter(node),
    ...customNodes.filter((d) => !d.part && !d.inline).map((d) => d.name),
  ];
  const inline = ['text', ...(node('hardBreak') ? ['hardBreak'] : []), ...customNodes.filter((d) => !d.part && d.inline).map((d) => d.name)].join('|');
  const any = blocks.join('|');

  const nodes: Record<string, NodeSpec> = {
    doc: { content: `(${any})+` },
    text: { group: 'inline' },
  };
  if (node('paragraph')) {
    nodes.paragraph = {
      attrs: { align: { default: null }, id: { default: null } },
      content: `(${inline})*`,
      group: 'block',
      parseDOM: [{ tag: 'p', getAttrs: blockAttrs }],
      toDOM: (n) => ['p', blockDOM(n), 0],
    };
  }
  if (node('heading')) {
    nodes.heading = {
      attrs: { level: { default: 2 }, align: { default: null }, id: { default: null } },
      content: `(${inline})*`,
      group: 'block',
      defining: true,
      // Pasted h1 and h5/h6 land on the levels the editor offers.
      parseDOM: [1, 2, 3, 4, 5, 6].map((l) => ({ tag: `h${l}`, getAttrs: (d: HTMLElement) => ({ level: Math.min(4, Math.max(2, l)), ...blockAttrs(d) }) })),
      toDOM: (n) => [headingTag(n.attrs.level), blockDOM(n), 0],
    };
  }
  if (node('bulletList')) {
    nodes.bulletList = { attrs: { tight: { default: null } }, content: 'listItem+', group: 'block', parseDOM: [{ tag: 'ul', getAttrs: (d) => ({ tight: tightOf(d) }) }], toDOM: (n) => listDOM('ul', n) };
  }
  if (node('orderedList')) {
    nodes.orderedList = {
      attrs: { start: { default: null }, tight: { default: null } },
      content: 'listItem+',
      group: 'block',
      parseDOM: [{ tag: 'ol', getAttrs: (d) => ({ start: startOf(d), tight: tightOf(d) }) }],
      toDOM: (n) => listDOM('ol', n),
    };
  }
  if (lists) {
    nodes.listItem = { content: `(${text.join('|')}) (${any})*`, defining: true, parseDOM: [{ tag: 'li' }], toDOM: () => ['li', 0] };
  }
  if (node('blockquote')) {
    nodes.blockquote = { content: `(${any})+`, group: 'block', defining: true, parseDOM: [{ tag: 'blockquote' }], toDOM: () => ['blockquote', 0] };
  }
  if (node('codeBlock')) {
    nodes.codeBlock = {
      attrs: { language: { default: null } },
      content: 'text*',
      marks: '',
      group: 'block',
      code: true,
      defining: true,
      parseDOM: [{ tag: 'pre', preserveWhitespace: 'full' }],
      toDOM: () => ['pre', ['code', 0]],
    };
  }
  if (node('horizontalRule')) {
    nodes.horizontalRule = { group: 'block', parseDOM: [{ tag: 'hr' }], toDOM: () => ['hr'] };
  }
  if (node('image')) {
    const attr = (d: HTMLElement, k: string) => d.getAttribute(k) || null;
    const size = (d: HTMLElement, k: string) => {
      const v = Number(d.getAttribute(k));
      return Number.isInteger(v) && v >= 1 ? v : null;
    };
    nodes.image = {
      attrs: { src: {}, alt: { default: null }, title: { default: null }, width: { default: null }, height: { default: null } },
      group: 'block',
      draggable: true,
      parseDOM: [
        {
          tag: 'img[src]',
          getAttrs: (d) => {
            const src = d.getAttribute('src') ?? '';
            return validImageSrc(src) ? { src, alt: attr(d, 'alt'), title: attr(d, 'title'), width: size(d, 'width'), height: size(d, 'height') } : false;
          },
        },
      ],
      toDOM: (n) => ['img', { src: n.attrs.src, alt: n.attrs.alt ?? '', title: n.attrs.title ?? undefined, width: n.attrs.width ?? undefined, height: n.attrs.height ?? undefined }],
    };
  }
  if (node('table')) {
    // Same attributes and roles as prosemirror-tables' tableNodes, so its commands and plugins work on them.
    const cellAttrs = {
      colspan: { default: 1 },
      rowspan: { default: 1 },
      colwidth: { default: null },
    };
    const cell = (tag: string, role: string): NodeSpec => ({
      attrs: cellAttrs,
      content: `(${any})+`,
      tableRole: role,
      isolating: true,
      parseDOM: [
        {
          tag,
          getAttrs: (d) => {
            const span = (k: string) => {
              const v = Number(d.getAttribute(k));
              return Number.isInteger(v) && v >= 1 && v <= 1000 ? v : 1;
            };
            const w = d.getAttribute('data-colwidth');
            const colwidth = w && /^\d+(,\d+)*$/.test(w) ? w.split(',').map(Number) : null;
            return { colspan: span('colspan'), rowspan: span('rowspan'), colwidth: colwidth && colwidth.length === span('colspan') ? colwidth : null };
          },
        },
      ],
      toDOM: (n) => {
        const a: Record<string, string> = {};
        if (n.attrs.colspan !== 1) a.colspan = String(n.attrs.colspan);
        if (n.attrs.rowspan !== 1) a.rowspan = String(n.attrs.rowspan);
        if (n.attrs.colwidth) a['data-colwidth'] = (n.attrs.colwidth as number[]).join(',');
        return [tag, a, 0];
      },
    });
    nodes.table = { content: 'tableRow+', group: 'block', tableRole: 'table', isolating: true, parseDOM: [{ tag: 'table' }], toDOM: () => ['table', ['tbody', 0]] };
    nodes.tableRow = { content: '(tableCell|tableHeader)*', tableRole: 'row', parseDOM: [{ tag: 'tr' }], toDOM: () => ['tr', 0] };
    nodes.tableCell = cell('td', 'cell');
    nodes.tableHeader = cell('th', 'header_cell');
  }
  if (node('hardBreak')) {
    nodes.hardBreak = { inline: true, group: 'inline', selectable: false, parseDOM: [{ tag: 'br' }], toDOM: () => ['br'] };
  }
  for (const d of customNodes) nodes[d.name] = customNode(d, any, inline);

  // Built-in and custom marks in the server's (alphabetical) order, which is the order marks are stored in.
  const marks: Record<string, MarkSpec> = {};
  if (mark('bold')) {
    marks.bold = {
      parseDOM: [{ tag: 'strong' }, { tag: 'b', getAttrs: (d) => (d.style.fontWeight !== 'normal' ? null : false) }, { style: 'font-weight=bold' }, { style: 'font-weight', getAttrs: (v) => (/^(bold(er)?|[5-9]\d{2,})$/.test(v) ? null : false) }],
      toDOM: () => ['strong', 0],
    };
  }
  if (mark('code')) marks.code = { code: true, parseDOM: [{ tag: 'code' }], toDOM: () => ['code', 0] };
  if (mark('italic')) {
    marks.italic = { parseDOM: [{ tag: 'i' }, { tag: 'em' }, { style: 'font-style=italic' }], toDOM: () => ['em', 0] };
  }
  if (mark('link')) {
    marks.link = {
      attrs: { href: {}, target: { default: null } },
      inclusive: false,
      parseDOM: [
        {
          tag: 'a[href]',
          getAttrs: (d) => (validHref(d.getAttribute('href') ?? '') ? { href: d.getAttribute('href'), target: d.getAttribute('target') === '_blank' ? '_blank' : null } : false),
        },
      ],
      toDOM: (m) => ['a', { href: m.attrs.href, ...(m.attrs.target === '_blank' ? { target: '_blank', rel: 'noopener noreferrer' } : { rel: 'noreferrer' }) }, 0],
    };
  }
  if (mark('strike')) {
    marks.strike = { parseDOM: [{ tag: 's' }, { tag: 'del' }, { tag: 'strike' }, { style: 'text-decoration=line-through' }], toDOM: () => ['s', 0] };
  }
  if (mark('subscript')) {
    marks.subscript = { excludes: 'superscript', parseDOM: [{ tag: 'sub' }, { style: 'vertical-align=sub' }], toDOM: () => ['sub', 0] };
  }
  if (mark('superscript')) {
    marks.superscript = { excludes: 'subscript', parseDOM: [{ tag: 'sup' }, { style: 'vertical-align=super' }], toDOM: () => ['sup', 0] };
  }
  if (mark('underline')) {
    marks.underline = { parseDOM: [{ tag: 'u' }, { style: 'text-decoration=underline' }], toDOM: () => ['u', 0] };
  }
  for (const d of custom?.marks ?? []) if (mark(d.name)) marks[d.name] = customMark(d);
  const sorted = Object.fromEntries(Object.entries(marks).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0)));
  return new Schema({ nodes, marks: sorted });
}

export interface StoredNode {
  type: string;
  attrs?: Record<string, unknown>;
  content?: StoredNode[];
  text?: string;
  marks?: { type: string; attrs?: Record<string, unknown> }[];
}

/** The stored form of a document: ProseMirror's JSON without null attributes, default-valued formatting and empty containers. */
export function toStored(doc: PMNode): StoredNode {
  const attrs = (type: string, a: Record<string, unknown> | undefined, mark: boolean) => {
    if (!a) return undefined;
    const kept = Object.entries(a).filter(([k, v]) => {
      if (v === null || v === undefined) return false;
      if (mark) return !(type === 'link' && k === 'target' && v !== '_blank');
      if (k === 'align' && (type === 'paragraph' || type === 'heading')) return v !== 'left';
      if (k === 'tight' && (type === 'bulletList' || type === 'orderedList')) return v === true;
      return !(type === 'orderedList' && k === 'start' && v === 1);
    });
    return kept.length ? Object.fromEntries(kept) : undefined;
  };
  const clean = (n: StoredNode): StoredNode => {
    const a = attrs(n.type, n.attrs, false);
    if (a) n.attrs = a;
    else delete n.attrs;
    n.marks?.forEach((m) => {
      const ma = attrs(m.type, m.attrs, true);
      if (ma) m.attrs = ma;
      else delete m.attrs;
    });
    n.content?.forEach(clean);
    if (n.content && n.content.length === 0) delete n.content;
    return n;
  };
  return clean(doc.toJSON() as StoredNode);
}

/** Mirrors the server's IsEmpty: no text, rule, image, break or custom leaf anywhere. */
export function isEmptyDoc(v: unknown, schema?: Schema): boolean {
  const has = (n: unknown): boolean => {
    if (!n || typeof n !== 'object') return false;
    const o = n as StoredNode;
    if (o.type === 'text') return !!o.text;
    if (o.type === 'horizontalRule' || o.type === 'hardBreak' || o.type === 'image') return true;
    if (schema?.nodes[o.type]?.isLeaf) return true;
    return (o.content ?? []).some(has);
  };
  return !has(v);
}

/** Parses a stored document; null when it is not valid for this schema. A missing or empty one gives an empty editor. */
export function fromStored(schema: Schema, value: unknown): PMNode | null {
  try {
    if (value === undefined || value === null || (typeof value === 'object' && isEmptyDoc(value, schema) && !hasContent(value))) {
      return schema.topNodeType.createAndFill();
    }
    const n = schema.nodeFromJSON(value);
    n.check();
    return n;
  } catch {
    return null;
  }
}

const hasContent = (v: unknown) => {
  const c = (v as StoredNode)?.content;
  return Array.isArray(c) && c.length > 0;
};
