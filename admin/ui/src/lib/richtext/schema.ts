import { Schema, type MarkSpec, type Node as PMNode, type NodeSpec } from 'prosemirror-model';

// The ProseMirror schema of a rich text field. Node and mark names and attributes are exactly what the server
// validates (schema/richtext.go): headings carry `level`, code blocks an optional `language`, links `href`,
// ordered lists nothing. Everything else is a pure editor concern.

export { configOf, type RichConfig } from './config';
import type { RichConfig } from './config';

/** href rules of the server: http(s) URL, mailto: address or item:<uuid>. */
export function validHref(h: string): boolean {
  if (h.startsWith('item:')) return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(h.slice(5));
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
  if (!v || /^[a-z][a-z0-9+.-]*:/i.test(v)) return v;
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

export function buildSchema(cfg: RichConfig = {}): Schema {
  const node = (n: string) => !cfg.nodes || cfg.nodes.includes(n);
  const mark = (m: string) => !cfg.marks || cfg.marks.includes(m);
  const lists = node('bulletList') || node('orderedList');

  const text = ['paragraph', 'heading'].filter(node);
  if (text.length === 0) throw new Error('a rich text field must allow paragraph or heading nodes');
  const blocks = [...text, ...['bulletList', 'orderedList', 'blockquote', 'codeBlock', 'horizontalRule', 'image', 'table'].filter(node)];
  const inline = ['text', ...(node('hardBreak') ? ['hardBreak'] : [])].join('|');
  const any = blocks.join('|');

  const nodes: Record<string, NodeSpec> = {
    doc: { content: `(${any})+` },
    text: { group: 'inline' },
  };
  if (node('paragraph')) {
    nodes.paragraph = { content: `(${inline})*`, group: 'block', parseDOM: [{ tag: 'p' }], toDOM: () => ['p', 0] };
  }
  if (node('heading')) {
    nodes.heading = {
      attrs: { level: { default: 2 } },
      content: `(${inline})*`,
      group: 'block',
      defining: true,
      // Pasted h1 and h5/h6 land on the levels the editor offers.
      parseDOM: [1, 2, 3, 4, 5, 6].map((l) => ({ tag: `h${l}`, attrs: { level: Math.min(4, Math.max(2, l)) } })),
      toDOM: (n) => [headingTag(n.attrs.level), 0],
    };
  }
  if (node('bulletList')) {
    nodes.bulletList = { content: 'listItem+', group: 'block', parseDOM: [{ tag: 'ul' }], toDOM: () => ['ul', 0] };
  }
  if (node('orderedList')) {
    nodes.orderedList = { content: 'listItem+', group: 'block', parseDOM: [{ tag: 'ol' }], toDOM: () => ['ol', 0] };
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

  // Declared in the server's (alphabetical) order, which is the order marks are stored in.
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
      attrs: { href: {} },
      inclusive: false,
      parseDOM: [{ tag: 'a[href]', getAttrs: (d) => (validHref(d.getAttribute('href') ?? '') ? { href: d.getAttribute('href') } : false) }],
      toDOM: (m) => ['a', { href: m.attrs.href, rel: 'noreferrer' }, 0],
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
  return new Schema({ nodes, marks });
}

export interface StoredNode {
  type: string;
  attrs?: Record<string, unknown>;
  content?: StoredNode[];
  text?: string;
  marks?: { type: string; attrs?: Record<string, unknown> }[];
}

/** The stored form of a document: ProseMirror's JSON without null attributes and empty containers. */
export function toStored(doc: PMNode): StoredNode {
  const clean = (n: StoredNode): StoredNode => {
    if (n.attrs) {
      const kept = Object.entries(n.attrs).filter(([, v]) => v !== null && v !== undefined);
      if (kept.length) n.attrs = Object.fromEntries(kept);
      else delete n.attrs;
    }
    n.content?.forEach(clean);
    if (n.content && n.content.length === 0) delete n.content;
    return n;
  };
  return clean(doc.toJSON() as StoredNode);
}

/** Mirrors the server's IsEmpty: no text, rule, image or break anywhere. */
export function isEmptyDoc(v: unknown): boolean {
  const has = (n: unknown): boolean => {
    if (!n || typeof n !== 'object') return false;
    const o = n as StoredNode;
    if (o.type === 'text') return !!o.text;
    if (o.type === 'horizontalRule' || o.type === 'hardBreak' || o.type === 'image') return true;
    return (o.content ?? []).some(has);
  };
  return !has(v);
}

/** Parses a stored document; null when it is not valid for this schema. A missing or empty one gives an empty editor. */
export function fromStored(schema: Schema, value: unknown): PMNode | null {
  try {
    if (value === undefined || value === null || (typeof value === 'object' && isEmptyDoc(value) && !hasContent(value))) {
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
