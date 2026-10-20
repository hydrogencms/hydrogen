import { baseKeymap, chainCommands, exitCode, lift, setBlockType, toggleMark, wrapIn } from 'prosemirror-commands';
import { history, redo, undo } from 'prosemirror-history';
import { InputRule, inputRules, textblockTypeInputRule, undoInputRule, wrappingInputRule } from 'prosemirror-inputrules';
import { keymap } from 'prosemirror-keymap';
import type { MarkType, NodeType, Schema } from 'prosemirror-model';
import { liftListItem, splitListItem, wrapInList } from 'prosemirror-schema-list';
import { EditorState, NodeSelection, Selection, type Command, type Plugin } from 'prosemirror-state';
import { goToNextCell, tableEditing } from 'prosemirror-tables';

/** An ancestor of the selection of one of the given types. */
export function inside(state: EditorState, type?: NodeType): boolean {
  if (!type) return false;
  const { $from } = state.selection;
  for (let d = $from.depth; d > 0; d--) if ($from.node(d).type === type) return true;
  return false;
}

export function markActive(state: EditorState, type?: MarkType): boolean {
  if (!type) return false;
  const { from, $from, to, empty } = state.selection;
  return empty ? !!type.isInSet(state.storedMarks ?? $from.marks()) : state.doc.rangeHasMark(from, to, type);
}

/** The textblock type at the selection head: paragraph, or heading with a level. */
export function blockOf(state: EditorState): { type: string; level?: number } {
  const n = state.selection.$from.parent;
  return { type: n.type.name, level: n.type.name === 'heading' ? (n.attrs.level as number) : undefined };
}

/** Toggles a textblock type: back to the first textblock type (paragraph) when already there. */
export function toggleBlock(schema: Schema, type: 'paragraph' | 'heading' | 'codeBlock', attrs?: Record<string, unknown>): Command {
  const t = schema.nodes[type];
  const para = schema.nodes.paragraph ?? schema.nodes.heading;
  return (state, dispatch) => {
    if (!t) return false;
    const b = blockOf(state);
    const same = b.type === type && (type !== 'heading' || b.level === attrs?.level);
    return setBlockType(same ? para : t, same ? undefined : attrs)(state, dispatch);
  };
}

/** Wraps in a list, or lifts out of it when the selection already sits in this kind of list. */
export function toggleList(schema: Schema, type: 'bulletList' | 'orderedList'): Command {
  const t = schema.nodes[type];
  return (state, dispatch) => {
    if (!t) return false;
    if (inside(state, t)) return liftListItem(schema.nodes.listItem)(state, dispatch);
    return wrapInList(t)(state, dispatch);
  };
}

export function toggleQuote(schema: Schema): Command {
  const t = schema.nodes.blockquote;
  return (state, dispatch) => {
    if (!t) return false;
    return inside(state, t) ? lift(state, dispatch) : wrapIn(t)(state, dispatch);
  };
}

export function insertRule(schema: Schema): Command {
  const t = schema.nodes.horizontalRule;
  return (state, dispatch) => {
    if (!t) return false;
    const { $from } = state.selection;
    const index = $from.index();
    if (!$from.parent.canReplaceWith(index, index, t)) {
      // Inside a textblock: place it after the block.
      const after = $from.after();
      if (!state.doc.resolve(after).parent.canReplaceWith(state.doc.resolve(after).index(), state.doc.resolve(after).index(), t)) return false;
      dispatch?.(state.tr.insert(after, t.create()).scrollIntoView());
      return true;
    }
    dispatch?.(state.tr.replaceSelectionWith(t.create()).scrollIntoView());
    return true;
  };
}

/** Where a block goes: after the textblock holding the selection, at the nearest level that accepts it. */
function blockSlot(state: EditorState, type: NodeType): number | null {
  const { $from, to } = state.selection;
  if (state.selection instanceof NodeSelection) {
    const $to = state.doc.resolve(to);
    return $to.parent.canReplaceWith($to.index(), $to.index(), type) ? to : null;
  }
  for (let d = $from.depth; d > 0; d--) {
    const i = $from.indexAfter(d - 1);
    if ($from.node(d - 1).canReplaceWith(i, i, type)) return $from.after(d);
  }
  return null;
}

export function insertImage(schema: Schema, attrs: { src: string; alt?: string; title?: string }): Command {
  const t = schema.nodes.image;
  return (state, dispatch) => {
    const at = t && blockSlot(state, t);
    if (at == null) return false;
    dispatch?.(state.tr.insert(at, t.create({ src: attrs.src, alt: attrs.alt || null, title: attrs.title || null })).scrollIntoView());
    return true;
  };
}

/** A table of rows x cols cells (the first row holds header cells), with the cursor in its first cell. */
export function insertTable(schema: Schema, rows = 3, cols = 3): Command {
  const { table, tableRow, tableCell, tableHeader } = schema.nodes;
  return (state, dispatch) => {
    const at = table && blockSlot(state, table);
    if (at == null) return false;
    if (dispatch) {
      const row = (type: NodeType) => tableRow.create(null, Array.from({ length: cols }, () => type.createAndFill()!));
      const node = table.create(null, Array.from({ length: rows }, (_, i) => row(i === 0 && tableHeader ? tableHeader : tableCell)));
      const tr = state.tr.insert(at, node);
      dispatch(tr.setSelection(Selection.near(tr.doc.resolve(at + 3))).scrollIntoView());
    }
    return true;
  };
}

const hardBreak = (schema: Schema): Command => (state, dispatch) => {
  const t = schema.nodes.hardBreak;
  if (!t || state.selection.$from.parent.type.spec.code) return false;
  dispatch?.(state.tr.replaceSelectionWith(t.create()).scrollIntoView());
  return true;
};

export function link(schema: Schema) {
  const type = schema.marks.link;
  interface Span {
    lo: number;
    hi: number;
    href: string;
  }
  /** The link under a cursor, as positions in the document. */
  const spanAt = (state: EditorState): (Span & { base: number }) | undefined => {
    const { $from } = state.selection;
    const off = $from.parentOffset;
    const spans: Span[] = [];
    let cur: Span | null = null;
    $from.parent.forEach((child, at) => {
      const mk = type.isInSet(child.marks);
      if (!mk) cur = null;
      else if (cur && cur.href === mk.attrs.href && cur.hi === at) cur.hi = at + child.nodeSize;
      else spans.push((cur = { lo: at, hi: at + child.nodeSize, href: mk.attrs.href as string }));
    });
    const s = spans.find((x) => off >= x.lo && off <= x.hi);
    return s && { ...s, base: $from.start() };
  };
  /** The href at the selection (cursor inside a link, or a selection covering one). */
  const hrefAt = (state: EditorState): string | undefined => {
    if (!type) return undefined;
    const { from, to, empty } = state.selection;
    if (empty) return spanAt(state)?.href;
    let found: string | undefined;
    state.doc.nodesBetween(from, to, (n) => {
      const mk = type.isInSet(n.marks);
      if (mk && found === undefined) found = mk.attrs.href as string;
    });
    return found;
  };
  /** The range a link edit applies to: the whole link under a cursor, else the selection. */
  const range = (state: EditorState): { from: number; to: number } => {
    const { from, to, empty } = state.selection;
    const s = empty && type ? spanAt(state) : undefined;
    return s ? { from: s.base + s.lo, to: s.base + s.hi } : { from, to };
  };
  const set =
    (href: string, label: string): Command =>
    (state, dispatch) => {
      if (!type) return false;
      const r = range(state);
      const mark = type.create({ href });
      let tr = state.tr;
      if (r.from === r.to) tr = tr.insertText(label, r.from).addMark(r.from, r.from + label.length, mark);
      else tr = tr.removeMark(r.from, r.to, type).addMark(r.from, r.to, mark);
      dispatch?.(tr.scrollIntoView());
      return true;
    };
  const remove: Command = (state, dispatch) => {
    if (!type || !hrefAt(state)) return false;
    const r = range(state);
    dispatch?.(state.tr.removeMark(r.from, r.to, type));
    return true;
  };
  return { hrefAt, set, remove };
}

export interface PluginOptions {
  schema: Schema;
  /** Opens the link popover (Mod-K). */
  openLink?: () => void;
  /** Replaces prosemirror-history (collaborative editing brings its own undo stack). */
  history?: { plugins: Plugin[]; undo: Command; redo: Command };
}

/** Keymaps, input rules and history for a schema; only what the schema allows is bound. */
export function plugins({ schema, openLink, history: h }: PluginOptions): Plugin[] {
  const { marks: m, nodes: n } = schema;
  const keys: Record<string, Command> = {
    'Mod-z': h?.undo ?? undo,
    'Shift-Mod-z': h?.redo ?? redo,
    'Mod-y': h?.redo ?? redo,
    Backspace: undoInputRule,
  };
  if (m.bold) keys['Mod-b'] = toggleMark(m.bold);
  if (m.italic) keys['Mod-i'] = toggleMark(m.italic);
  if (m.code) keys['Mod-e'] = toggleMark(m.code);
  if (m.underline) keys['Mod-u'] = toggleMark(m.underline);
  if (m.strike) keys['Shift-Mod-x'] = toggleMark(m.strike);
  if (m.superscript) keys['Mod-.'] = toggleMark(m.superscript);
  if (m.subscript) keys['Mod-,'] = toggleMark(m.subscript);
  if (m.link && openLink) {
    keys['Mod-k'] = () => {
      openLink();
      return true;
    };
  }
  if (n.heading) {
    keys['Mod-Alt-2'] = toggleBlock(schema, 'heading', { level: 2 });
    keys['Mod-Alt-3'] = toggleBlock(schema, 'heading', { level: 3 });
    keys['Mod-Alt-4'] = toggleBlock(schema, 'heading', { level: 4 });
  }
  if (n.paragraph) keys['Mod-Alt-0'] = setBlockType(n.paragraph);
  if (n.bulletList) keys['Shift-Ctrl-8'] = toggleList(schema, 'bulletList');
  if (n.orderedList) keys['Shift-Ctrl-9'] = toggleList(schema, 'orderedList');
  if (n.hardBreak) keys['Shift-Enter'] = chainCommands(exitCode, hardBreak(schema));
  if (n.listItem) keys.Enter = splitListItem(n.listItem);
  if (n.table) {
    keys.Tab = goToNextCell(1);
    keys['Shift-Tab'] = goToNextCell(-1);
  }

  const rules: InputRule[] = [];
  if (n.heading) {
    rules.push(
      textblockTypeInputRule(/^(#{2,4})\s$/, n.heading, (match) => ({ level: match[1].length })),
    );
  }
  if (n.bulletList) rules.push(wrappingInputRule(/^\s*([-*])\s$/, n.bulletList));
  if (n.orderedList) rules.push(wrappingInputRule(/^(\d+)\.\s$/, n.orderedList));
  if (n.blockquote) rules.push(wrappingInputRule(/^\s*>\s$/, n.blockquote));
  if (n.codeBlock) rules.push(textblockTypeInputRule(/^```$/, n.codeBlock));

  return [inputRules({ rules }), keymap(keys), keymap(baseKeymap), ...(n.table ? [tableEditing()] : []), ...(h?.plugins ?? [history()])];
}
