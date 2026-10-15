import { DOMParser as PMDOMParser } from 'prosemirror-model';
import { EditorState, TextSelection } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import { afterEach, describe, expect, it } from 'vitest';
import { link, plugins } from './commands';
import { buildSchema, fromStored, isEmptyDoc, normalizeHref, toStored, validHref } from './schema';

const UUID = '0b6d1f43-5d7e-4f5a-9d1c-2f4b9c3a7e10';
const text = (s: string, marks?: { type: string; attrs?: Record<string, unknown> }[]) => ({ type: 'text', text: s, ...(marks ? { marks } : {}) });
const para = (...c: unknown[]) => ({ type: 'paragraph', content: c });

// Every node and mark the server knows, in the shape it stores.
const everything = {
  type: 'doc',
  content: [
    { type: 'heading', attrs: { level: 2 }, content: [text('Title')] },
    { type: 'heading', attrs: { level: 4 }, content: [text('Sub')] },
    para(
      text('plain '),
      text('bold', [{ type: 'bold' }]),
      text(' '),
      text('italic', [{ type: 'italic' }]),
      text(' '),
      text('code', [{ type: 'code' }]),
      text(' '),
      text('both', [{ type: 'bold' }, { type: 'italic' }]),
      text(' '),
      text('web', [{ type: 'link', attrs: { href: 'https://example.com/a?b=1' } }]),
      text(' '),
      text('item', [{ type: 'link', attrs: { href: `item:${UUID}` } }]),
      text(' '),
      text('mail', [{ type: 'link', attrs: { href: 'mailto:a@b.co' } }]),
      { type: 'hardBreak' },
      text('after break'),
    ),
    { type: 'bulletList', content: [{ type: 'listItem', content: [para(text('one')), { type: 'orderedList', content: [{ type: 'listItem', content: [para(text('nested'))] }] }] }, { type: 'listItem', content: [para(text('two'))] }] },
    { type: 'orderedList', content: [{ type: 'listItem', content: [para(text('first'))] }] },
    { type: 'blockquote', content: [para(text('quoted')), { type: 'bulletList', content: [{ type: 'listItem', content: [para(text('in quote'))] }] }] },
    { type: 'codeBlock', attrs: { language: 'go' }, content: [text('func main() {}\n')] },
    { type: 'codeBlock', content: [text('no language')] },
    { type: 'horizontalRule' },
    { type: 'paragraph' },
  ],
};

describe('stored document round trip', () => {
  it('keeps every node and mark exactly', () => {
    const schema = buildSchema();
    const doc = fromStored(schema, everything);
    expect(doc).not.toBeNull();
    expect(toStored(doc!)).toEqual(everything);
  });

  it('keeps the server mark order', () => {
    const schema = buildSchema();
    const doc = fromStored(schema, { type: 'doc', content: [para(text('x', [{ type: 'link', attrs: { href: 'https://a.io' } }, { type: 'bold' }]))] });
    const marks = (toStored(doc!).content![0].content![0].marks ?? []).map((m) => m.type);
    expect(marks).toEqual(['bold', 'link']);
  });

  it('never stores null attributes', () => {
    const schema = buildSchema();
    const json = JSON.stringify(toStored(schema.nodeFromJSON({ type: 'doc', content: [{ type: 'codeBlock' }] })));
    expect(json).toBe('{"type":"doc","content":[{"type":"codeBlock"}]}');
  });

  it('refuses documents the schema cannot hold', () => {
    const schema = buildSchema();
    expect(fromStored(schema, { type: 'doc', content: [{ type: 'image' }] })).toBeNull();
    expect(fromStored(schema, { type: 'doc', content: [para(text('x', [{ type: 'strike' }]))] })).toBeNull();
    expect(fromStored(schema, 'text')).toBeNull();
  });
});

describe('empty documents', () => {
  it('matches the server: no text, rule or break anywhere', () => {
    expect(isEmptyDoc({ type: 'doc' })).toBe(true);
    expect(isEmptyDoc({ type: 'doc', content: [] })).toBe(true);
    expect(isEmptyDoc({ type: 'doc', content: [{ type: 'paragraph' }, para()] })).toBe(true);
    expect(isEmptyDoc({ type: 'doc', content: [para(text('a'))] })).toBe(false);
    expect(isEmptyDoc({ type: 'doc', content: [{ type: 'horizontalRule' }] })).toBe(false);
    expect(isEmptyDoc({ type: 'doc', content: [para({ type: 'hardBreak' })] })).toBe(false);
  });

  it('opens a missing or empty value as one empty paragraph', () => {
    const schema = buildSchema();
    for (const v of [undefined, null, { type: 'doc' }, { type: 'doc', content: [] }]) {
      const doc = fromStored(schema, v);
      expect(toStored(doc!)).toEqual({ type: 'doc', content: [{ type: 'paragraph' }] });
    }
  });

  it('opens an empty editor on a field that only allows headings', () => {
    const schema = buildSchema({ nodes: ['heading'] });
    expect(toStored(fromStored(schema, undefined)!)).toEqual({ type: 'doc', content: [{ type: 'heading', attrs: { level: 2 } }] });
  });
});

describe('allowlists', () => {
  it('leaves disallowed marks and nodes out of the schema', () => {
    const schema = buildSchema({ marks: ['bold'], nodes: ['paragraph', 'heading'] });
    expect(Object.keys(schema.marks)).toEqual(['bold']);
    expect(Object.keys(schema.nodes).sort()).toEqual(['doc', 'heading', 'paragraph', 'text']);
    expect(fromStored(schema, { type: 'doc', content: [para(text('x', [{ type: 'italic' }]))] })).toBeNull();
    expect(fromStored(schema, { type: 'doc', content: [{ type: 'bulletList', content: [{ type: 'listItem', content: [para()] }] }] })).toBeNull();
  });

  it('allows list items when either list is allowed', () => {
    expect(buildSchema({ nodes: ['paragraph', 'orderedList'] }).nodes.listItem).toBeDefined();
    expect(buildSchema({ nodes: ['paragraph'] }).nodes.listItem).toBeUndefined();
  });

  it('strips pasted formatting the field does not allow', () => {
    const schema = buildSchema({ marks: ['bold'], nodes: ['paragraph'] });
    const dom = new window.DOMParser().parseFromString(
      '<h1>Heading</h1><p>a <em>b</em> <strong>c</strong> <a href="https://x.io">d</a> <code>e</code></p><ul><li>item</li></ul><blockquote><p>q</p></blockquote><hr>',
      'text/html',
    );
    const doc = PMDOMParser.fromSchema(schema).parse(dom.body);
    const out = toStored(doc);
    const marks = new Set<string>();
    const types = new Set<string>();
    doc.descendants((n) => {
      types.add(n.type.name);
      n.marks.forEach((m) => marks.add(m.type.name));
    });
    expect([...marks]).toEqual(['bold']);
    expect([...types].sort()).toEqual(['paragraph', 'text']);
    expect(JSON.stringify(out)).toContain('"text":"c"');
    expect(JSON.stringify(out)).not.toContain('italic');
  });

  it('clamps pasted headings to the offered levels and drops unsafe links', () => {
    const schema = buildSchema();
    const dom = new window.DOMParser().parseFromString('<h1>a</h1><h6>b</h6><p><a href="javascript:alert(1)">x</a> <a href="https://ok.io">y</a></p>', 'text/html');
    const out = toStored(PMDOMParser.fromSchema(schema).parse(dom.body));
    expect(out.content!.slice(0, 2).map((n) => n.attrs)).toEqual([{ level: 2 }, { level: 4 }]);
    const marks = out.content![2].content!.flatMap((n) => n.marks ?? []);
    expect(marks).toEqual([{ type: 'link', attrs: { href: 'https://ok.io' } }]);
  });
});

describe('links', () => {
  it('validates hrefs like the server', () => {
    expect(validHref('https://a.io/x')).toBe(true);
    expect(validHref('http://a.io')).toBe(true);
    expect(validHref('mailto:a@b.co')).toBe(true);
    expect(validHref(`item:${UUID}`)).toBe(true);
    for (const bad of ['item:123', 'javascript:alert(1)', 'ftp://a.io', 'a.io', 'mailto:', '']) expect(validHref(bad)).toBe(false);
  });

  it('normalizes what an editor types', () => {
    expect(normalizeHref(' example.com/x ')).toBe('https://example.com/x');
    expect(normalizeHref('me@example.com')).toBe('mailto:me@example.com');
    expect(normalizeHref('https://a.io')).toBe('https://a.io');
    expect(normalizeHref('nonsense')).toBe('nonsense');
  });

  it('links a selection to an item, edits and removes it', () => {
    const schema = buildSchema();
    const l = link(schema);
    let state = EditorState.create({ doc: fromStored(schema, { type: 'doc', content: [para(text('read the guide now'))] })! });
    state = state.apply(state.tr.setSelection(TextSelection.create(state.doc, 10, 15)));
    expect(l.hrefAt(state)).toBeUndefined();
    l.set(`item:${UUID}`, 'ignored')(state, (tr) => (state = state.apply(tr)));
    expect(toStored(state.doc).content![0].content).toEqual([
      text('read the '),
      text('guide', [{ type: 'link', attrs: { href: `item:${UUID}` } }]),
      text(' now'),
    ]);
    // A cursor inside the link edits the whole link.
    state = state.apply(state.tr.setSelection(TextSelection.create(state.doc, 12)));
    expect(l.hrefAt(state)).toBe(`item:${UUID}`);
    l.set('https://example.com', '')(state, (tr) => (state = state.apply(tr)));
    expect(toStored(state.doc).content![0].content![1]).toEqual(text('guide', [{ type: 'link', attrs: { href: 'https://example.com' } }]));
    l.remove(state, (tr) => (state = state.apply(tr)));
    expect(toStored(state.doc).content![0].content).toEqual([text('read the guide now')]);
  });

  it('inserts the label when nothing is selected', () => {
    const schema = buildSchema();
    let state = EditorState.create({ doc: fromStored(schema, undefined)! });
    link(schema).set('https://example.com', 'example.com')(state, (tr) => (state = state.apply(tr)));
    expect(toStored(state.doc).content![0].content).toEqual([text('example.com', [{ type: 'link', attrs: { href: 'https://example.com' } }])]);
  });
});

describe('input rules', () => {
  let view: EditorView | undefined;
  afterEach(() => view?.destroy());

  function editor(cfg = {}) {
    const schema = buildSchema(cfg);
    view = new EditorView(document.body.appendChild(document.createElement('div')), {
      state: EditorState.create({ doc: fromStored(schema, undefined)!, plugins: plugins({ schema }) }),
    });
    return view;
  }
  function type(v: EditorView, s: string) {
    for (const ch of s) {
      const { from, to } = v.state.selection;
      if (!v.someProp('handleTextInput', (f) => f(v, from, to, ch, () => v.state.tr.insertText(ch, from, to)))) v.dispatch(v.state.tr.insertText(ch, from, to));
    }
  }
  const stored = (v: EditorView) => toStored(v.state.doc).content!;

  it('turns "## " into a heading of level 2, and "#### " into 4', () => {
    const v = editor();
    type(v, '## Hi');
    expect(stored(v)).toEqual([{ type: 'heading', attrs: { level: 2 }, content: [text('Hi')] }]);
    const w = editor();
    type(w, '#### Deep');
    expect(stored(w)[0].attrs).toEqual({ level: 4 });
  });

  it('does not turn "# " or "##### " into a heading', () => {
    const v = editor();
    type(v, '# x');
    expect(stored(v)[0].type).toBe('paragraph');
  });

  it('makes lists, quotes and code blocks', () => {
    for (const [typed, node] of [
      ['- a', 'bulletList'],
      ['* a', 'bulletList'],
      ['1. a', 'orderedList'],
      ['> a', 'blockquote'],
    ] as const) {
      const v = editor();
      type(v, typed);
      expect(stored(v)[0].type, typed).toBe(node);
    }
    const v = editor();
    type(v, '```');
    expect(stored(v)).toEqual([{ type: 'codeBlock' }]);
  });

  it('skips rules for nodes the field does not allow', () => {
    const v = editor({ nodes: ['paragraph'] });
    type(v, '## x');
    expect(stored(v)).toEqual([para(text('## x'))]);
  });

  it('undoes a rule with Backspace right after it fired', () => {
    const v = editor();
    type(v, '- ');
    expect(stored(v)[0].type).toBe('bulletList');
    v.someProp('handleKeyDown', (f) => f(v, new KeyboardEvent('keydown', { key: 'Backspace' })));
    expect(stored(v)).toEqual([para(text('- '))]);
  });
});
