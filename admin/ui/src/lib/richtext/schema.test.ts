import { DOMParser as PMDOMParser, DOMSerializer } from 'prosemirror-model';
import { EditorState, TextSelection, type Transaction } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import { afterEach, describe, expect, it } from 'vitest';
import { applyMark, customAt, docIds, insertCustom, insertImage, insertTable, link, listAt, plugins, setAlign, setAnchor, setNodeAttrs } from './commands';
import type { RichAttrDef, RichCustom } from './config';
import { buildSchema, fromStored, isEmptyDoc, normalizeHref, toStored, validHref, validImageSrc } from './schema';

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
    expect(fromStored(schema, { type: 'doc', content: [{ type: 'video' }] })).toBeNull();
    expect(fromStored(schema, { type: 'doc', content: [para(text('x', [{ type: 'blink' }]))] })).toBeNull();
    expect(fromStored(schema, 'text')).toBeNull();
  });
});

describe('tables, images and more marks', () => {
  const cell = (type: string, attrs: Record<string, unknown>, ...c: unknown[]) => ({ type, attrs, content: c });
  const rich = {
    type: 'doc',
    content: [
      para(
        text('u', [{ type: 'strike' }, { type: 'underline' }]),
        text('2', [{ type: 'superscript' }]),
        text('3', [{ type: 'subscript' }]),
      ),
      {
        type: 'table',
        content: [
          { type: 'tableRow', content: [cell('tableHeader', { colspan: 2, rowspan: 1 }, para(text('h')))] },
          { type: 'tableRow', content: [cell('tableCell', { colspan: 1, rowspan: 2, colwidth: [100] }, para(text('a')), { type: 'image', attrs: { src: '/assets/x/y.png', alt: 'A', width: 10 } })] },
        ],
      },
      { type: 'image', attrs: { src: 'https://a.b/c.png' } },
    ],
  };

  it('keeps them exactly', () => {
    const doc = fromStored(buildSchema(), rich);
    expect(doc).not.toBeNull();
    expect(toStored(doc!)).toEqual(rich);
  });

  it('the table allow-list covers rows and cells; image and table are separate', () => {
    const only = buildSchema({ nodes: ['paragraph', 'table'] });
    expect(Object.keys(only.nodes)).toEqual(expect.arrayContaining(['table', 'tableRow', 'tableCell', 'tableHeader']));
    expect(only.nodes.image).toBeUndefined();
    expect(buildSchema({ nodes: ['paragraph', 'image'] }).nodes.table).toBeUndefined();
    expect(buildSchema({ marks: ['underline'] }).marks.strike).toBeUndefined();
  });

  it('counts an image as content', () => {
    expect(isEmptyDoc({ type: 'doc', content: [{ type: 'image', attrs: { src: '/a.png' } }] })).toBe(false);
  });

  it('validates image sources like the server', () => {
    for (const ok of ['https://a.b/c.png', 'http://a.b/c', '/assets/x/y.png']) expect(validImageSrc(ok)).toBe(true);
    for (const bad of ['', 'javascript:alert(1)', 'data:image/png;base64,AA', '//evil.com/a.png', '/\\evil', 'a.png', 'ftp://a.b/c']) expect(validImageSrc(bad)).toBe(false);
  });

  it('pastes tables and drops unsafe images', () => {
    const schema = buildSchema();
    const host = document.createElement('div');
    host.innerHTML = '<table><tr><th colspan="2">h</th></tr><tr><td>a</td><td rowspan="2">b</td></tr></table><img src="javascript:x"><img src="/a.png" alt="A" width="5">';
    const stored = toStored(PMDOMParser.fromSchema(schema).parse(host));
    expect(stored.content![0].type).toBe('table');
    expect(stored.content![0].content![0].content![0].attrs).toEqual({ colspan: 2, rowspan: 1 });
    expect(stored.content!.filter((n) => n.type === 'image')).toEqual([{ type: 'image', attrs: { src: '/a.png', alt: 'A', width: 5 } }]);
  });

  it('inserts a table and an image at the cursor and moves with Tab', () => {
    const schema = buildSchema();
    let state = EditorState.create({ doc: fromStored(schema, null)!, plugins: plugins({ schema }) });
    const run = (c: (s: EditorState, d: (tr: import('prosemirror-state').Transaction) => void) => boolean) => c(state, (tr) => (state = state.apply(tr)));
    expect(run(insertTable(schema, 2, 2))).toBe(true);
    expect(state.doc.firstChild!.type.name).toBe('paragraph');
    expect(state.doc.child(1).type.name).toBe('table');
    expect(state.selection.$from.parent.type.name).toBe('paragraph');
    expect(state.selection.$from.node(-1).type.name).toBe('tableHeader');
    expect(run(insertImage(schema, { src: '/a.png', alt: 'A' }))).toBe(true);
    expect(state.selection.$from.node(-1).firstChild!.type.name).toBe('paragraph');
    expect(state.selection.$from.node(-1).lastChild!.type.name).toBe('image');
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

  it('starts a numbered list at the typed number', () => {
    const v = editor();
    type(v, '5. five');
    expect(stored(v)[0]).toMatchObject({ type: 'orderedList', attrs: { start: 5 } });
    const w = editor();
    type(w, '1. one');
    expect(stored(w)[0].attrs).toBeUndefined();
    expect(listAt(v.state)?.node.attrs.start).toBe(5);
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

const attr = (name: string, type: RichAttrDef['type'], extra: Partial<RichAttrDef> = {}): RichAttrDef => ({ name, type, required: false, ...extra });
const custom: RichCustom = {
  nodes: [
    { name: 'callout', label: 'Callout', tag: 'aside', inline: false, content: 'blocks', parts: [], part: false, attrs: [attr('variant', 'enum', { values: ['info', 'warning'], default: 'info' }), attr('open', 'bool')] },
    { name: 'details', label: 'Accordion', tag: 'details', inline: false, content: 'parts', parts: ['detailsSummary', 'detailsBody'], part: false, attrs: [] },
    { name: 'detailsSummary', label: '', tag: 'summary', inline: false, content: 'inline', parts: [], part: true, attrs: [] },
    { name: 'detailsBody', label: '', tag: 'div', inline: false, content: 'blocks', parts: [], part: true, attrs: [] },
    { name: 'embed', label: 'Embed', tag: 'figure', inline: false, content: 'none', parts: [], part: false, attrs: [attr('url', 'url', { required: true }), attr('caption', 'string', { maxLength: 20 })] },
    { name: 'footnoteRef', label: 'Footnote', tag: 'span', inline: true, content: 'none', parts: [], part: false, attrs: [attr('n', 'int', { min: 1, max: 999, required: true })] },
  ],
  marks: [{ name: 'highlight', label: 'Highlight', tag: 'mark', attrs: [attr('tone', 'enum', { values: ['yellow', 'green'] })] }],
};
const stateOf = (schema: ReturnType<typeof buildSchema>, json: unknown) => EditorState.create({ doc: fromStored(schema, json)!, plugins: plugins({ schema }) });
const parse = (schema: ReturnType<typeof buildSchema>, html: string) => {
  const host = document.createElement('div');
  host.innerHTML = html;
  return toStored(PMDOMParser.fromSchema(schema).parse(host));
};
const serialize = (schema: ReturnType<typeof buildSchema>, json: unknown) => {
  const out = DOMSerializer.fromSchema(schema).serializeNode(schema.nodeFromJSON(json));
  const host = document.createElement('div');
  host.append(out);
  return host.innerHTML;
};

describe('custom nodes and marks', () => {
  const schema = buildSchema({}, custom);
  const doc = {
    type: 'doc',
    content: [
      { type: 'callout', attrs: { variant: 'warning' }, content: [para(text('careful'))] },
      { type: 'details', content: [{ type: 'detailsSummary', content: [text('more')] }, { type: 'detailsBody', content: [para(text('body'))] }] },
      { type: 'embed', attrs: { url: 'https://a.b/v', caption: 'A video' } },
      para(text('see '), { type: 'footnoteRef', attrs: { n: 3 } }, text(' and '), text('this', [{ type: 'highlight', attrs: { tone: 'green' } }])),
    ],
  };

  it('keeps them exactly, parts and inline atoms included', () => {
    expect(toStored(fromStored(schema, doc)!)).toEqual(doc);
  });

  it('keeps attrs the document left out absent (no defaults written on load)', () => {
    const bare = { type: 'doc', content: [{ type: 'callout', content: [para(text('x'))] }] };
    expect(toStored(fromStored(schema, bare)!)).toEqual(bare);
  });

  it('builds the specs: groups, atoms, parts outside any group', () => {
    expect(schema.nodes.callout.spec.group).toBe('block');
    expect(schema.nodes.embed.isAtom).toBe(true);
    expect(schema.nodes.footnoteRef.isInline && schema.nodes.footnoteRef.isAtom).toBe(true);
    expect(schema.nodes.detailsSummary.spec.group).toBeUndefined();
    expect(schema.nodes.details.contentMatch.defaultType).toBe(schema.nodes.detailsSummary);
    expect(schema.nodes.paragraph.contentMatch.matchType(schema.nodes.footnoteRef)).not.toBeNull();
    expect(fromStored(schema, { type: 'doc', content: [{ type: 'detailsBody', content: [para()] }] })).toBeNull();
    expect(fromStored(schema, { type: 'doc', content: [{ type: 'details', content: [{ type: 'detailsBody', content: [para()] }] }] })).toBeNull();
  });

  it('orders marks by name among the built-in ones', () => {
    expect(Object.keys(schema.marks)).toEqual(['bold', 'code', 'highlight', 'italic', 'link', 'strike', 'subscript', 'superscript', 'underline']);
    const doc2 = fromStored(schema, { type: 'doc', content: [para(text('x', [{ type: 'italic' }, { type: 'highlight', attrs: { tone: 'green' } }, { type: 'bold' }]))] });
    expect(toStored(doc2!).content![0].content![0].marks!.map((m) => m.type)).toEqual(['bold', 'highlight', 'italic']);
  });

  it('renders the default markup: tag, rt- class, data attributes', () => {
    expect(serialize(schema, { type: 'callout', attrs: { variant: 'warning', open: true }, content: [para(text('x'))] })).toBe('<aside class="rt-callout" data-variant="warning" data-open=""><p>x</p></aside>');
    expect(serialize(schema, { type: 'callout', content: [para(text('x'))] })).toContain('data-variant="info"');
    expect(serialize(schema, para(text('x', [{ type: 'highlight', attrs: { tone: 'green' } }])))).toBe('<p><mark class="rt-highlight" data-tone="green">x</mark></p>');
    expect(serialize(schema, { type: 'details', content: [{ type: 'detailsSummary', content: [text('s')] }, { type: 'detailsBody', content: [para()] }] })).toContain('<details class="rt-details"><summary class="rt-detailsSummary">s</summary>');
  });

  it('parses its own markup back (copy and paste)', () => {
    for (const j of doc.content) {
      const html = serialize(schema, j);
      expect(parse(schema, html).content![0]).toEqual(j);
    }
  });

  it('drops pasted nodes whose required attributes are missing or invalid', () => {
    const stored = parse(schema, '<figure class="rt-embed" data-url="javascript:alert(1)"></figure><p>kept</p>');
    expect(stored.content!.map((n) => n.type)).toEqual(['paragraph']);
    expect(parse(schema, '<p><span class="rt-footnoteRef" data-n="0"></span>x</p>').content![0].content![0].type).toBe('text');
    expect(parse(schema, '<aside class="rt-callout" data-variant="bogus"><p>x</p></aside>').content![0]).toEqual({ type: 'callout', content: [para(text('x'))] });
  });

  it('allowlists: a name allows it, parts follow the parent, a nil list allows all', () => {
    const only = buildSchema({ nodes: ['paragraph', 'details'], marks: [] }, custom);
    expect(only.nodes.details).toBeDefined();
    expect(only.nodes.detailsSummary).toBeDefined();
    expect(only.nodes.detailsBody).toBeDefined();
    expect(only.nodes.callout).toBeUndefined();
    expect(only.nodes.embed).toBeUndefined();
    expect(only.nodes.footnoteRef).toBeUndefined();
    expect(only.marks.highlight).toBeUndefined();
    expect(buildSchema({ marks: ['highlight'] }, custom).marks.highlight).toBeDefined();
    expect(buildSchema({ nodes: ['paragraph', 'detailsSummary'] }, custom).nodes.detailsSummary).toBeUndefined();
    expect(buildSchema({}, custom).nodes.callout).toBeDefined();
  });

  it('a custom leaf counts as content', () => {
    const v = { type: 'doc', content: [{ type: 'embed', attrs: { url: '/a' } }] };
    expect(isEmptyDoc(v)).toBe(true);
    expect(isEmptyDoc(v, schema)).toBe(false);
    expect(isEmptyDoc({ type: 'doc', content: [{ type: 'details', content: [{ type: 'detailsSummary' }, { type: 'detailsBody', content: [para()] }] }] }, schema)).toBe(true);
  });

  it('inserts nodes with their parts and defaults, and edits their attributes', () => {
    let st = stateOf(schema, { type: 'doc', content: [para(text('hi'))] });
    const run = (c: (s: EditorState, d: (t: Transaction) => void) => boolean) => c(st, (tr) => (st = st.apply(tr)));
    expect(run(insertCustom(schema, custom.nodes[1], {}))).toBe(true);
    expect(toStored(st.doc).content!.map((n) => n.type)).toEqual(['paragraph', 'details']);
    expect(toStored(st.doc).content![1]).toEqual({ type: 'details', content: [{ type: 'detailsSummary' }, { type: 'detailsBody', content: [{ type: 'paragraph' }] }] });
    expect(run(insertCustom(schema, custom.nodes[0], { variant: 'info' }))).toBe(true);
    const at = customAt(st, new Map(custom.nodes.map((d) => [d.name, d])));
    expect(at?.def.name).toBe('callout');
    expect(run(setNodeAttrs(at!.pos, { variant: 'warning' }))).toBe(true);
    expect(JSON.stringify(toStored(st.doc))).toContain('"variant":"warning"');
    expect(run(insertCustom(schema, custom.nodes[4], { url: '/a.png' }))).toBe(true);
    expect(JSON.stringify(toStored(st.doc))).toContain('{"type":"embed","attrs":{"url":"/a.png"}}');
  });

  it('inserts an inline atom inside text and refuses it where no text fits', () => {
    let st = stateOf(schema, { type: 'doc', content: [para(text('hi'))] });
    st = st.apply(st.tr.setSelection(TextSelection.create(st.doc, 2)));
    expect(insertCustom(schema, custom.nodes[5], { n: 2 })(st, (tr) => (st = st.apply(tr)))).toBe(true);
    expect(toStored(st.doc).content![0].content!.map((n) => n.type)).toEqual(['text', 'footnoteRef', 'text']);
    const code = stateOf(buildSchema({}, custom), { type: 'doc', content: [{ type: 'codeBlock', content: [text('x')] }] });
    expect(insertCustom(schema, custom.nodes[5], { n: 1 })(code.apply(code.tr.setSelection(TextSelection.create(code.doc, 2))))).toBe(false);
  });

  it('applies a mark with attributes over a selection', () => {
    let st = stateOf(schema, { type: 'doc', content: [para(text('hello'))] });
    st = st.apply(st.tr.setSelection(TextSelection.create(st.doc, 1, 4)));
    applyMark(schema.marks.highlight, { tone: 'yellow' })(st, (tr) => (st = st.apply(tr)));
    expect(toStored(st.doc).content![0].content).toEqual([text('hel', [{ type: 'highlight', attrs: { tone: 'yellow' } }]), text('lo')]);
  });
});

describe('core formatting', () => {
  const schema = buildSchema();
  it('stores alignment, anchors, list start and tight like the server validates them', () => {
    const v = {
      type: 'doc',
      content: [
        { type: 'paragraph', attrs: { align: 'center', id: 'intro' }, content: [text('x')] },
        { type: 'heading', attrs: { level: 3, align: 'justify', id: 'h-2' }, content: [text('y')] },
        { type: 'orderedList', attrs: { start: 4, tight: true }, content: [{ type: 'listItem', content: [para(text('a'))] }] },
        { type: 'bulletList', attrs: { tight: true }, content: [{ type: 'listItem', content: [para(text('a'))] }] },
        para(text('l', [{ type: 'link', attrs: { href: '#intro', target: '_blank' } }]), text('m', [{ type: 'link', attrs: { href: `item:${UUID}#h-2` } }])),
      ],
    };
    expect(toStored(fromStored(schema, v)!)).toEqual(v);
  });

  it('never stores values that are the default', () => {
    const v = {
      type: 'doc',
      content: [
        { type: 'paragraph', attrs: { align: 'left' }, content: [text('x')] },
        { type: 'orderedList', attrs: { start: 1, tight: false }, content: [{ type: 'listItem', content: [para(text('a'))] }] },
        { type: 'bulletList', attrs: { tight: false }, content: [{ type: 'listItem', content: [para(text('a'))] }] },
        para(text('l', [{ type: 'link', attrs: { href: 'https://a.b', target: 'self' } }])),
      ],
    };
    const out = toStored(fromStored(schema, v)!);
    expect(out.content![0]).toEqual(para(text('x')));
    expect(out.content![1]).toEqual({ type: 'orderedList', content: [{ type: 'listItem', content: [para(text('a'))] }] });
    expect(out.content![2]).toEqual({ type: 'bulletList', content: [{ type: 'listItem', content: [para(text('a'))] }] });
    expect(out.content![3].content![0].marks).toEqual([{ type: 'link', attrs: { href: 'https://a.b' } }]);
  });

  it('validates the new href forms', () => {
    for (const ok of ['#top', '#a_b-1', `item:${UUID}#sec`, `item:${UUID}`]) expect(validHref(ok)).toBe(true);
    for (const bad of ['#', '#1a', '#a.b', '#a b', `item:${UUID}#`, `item:${UUID}#1`, `item:${UUID}#a#b`, 'item:#a']) expect(validHref(bad)).toBe(false);
    expect(normalizeHref('#intro')).toBe('#intro');
    expect(normalizeHref('#a.b')).toBe('#a.b');
  });

  it('pastes alignment, ids, list start, link targets and compact lists', () => {
    const stored = parse(
      schema,
      '<p style="text-align: center" id="a1">c</p><h3 style="text-align:left" id="1bad">h</h3><p style="text-align:right">r</p>' +
        '<ol start="3"><li>x</li><li>y</li></ol><ol start="1"><li><p>z</p></li></ol><ul><li>t</li></ul><ul><li>a<ul><li>n</li></ul></li></ul>' +
        '<p><a href="https://a.b" target="_blank">out</a><a href="#a1" target="_self">in</a></p>',
    ).content!;
    expect(stored[0]).toEqual({ type: 'paragraph', attrs: { align: 'center', id: 'a1' }, content: [text('c')] });
    expect(stored[1]).toEqual({ type: 'heading', attrs: { level: 3 }, content: [text('h')] });
    expect(stored[2].attrs).toEqual({ align: 'right' });
    expect(stored[3].attrs).toEqual({ start: 3, tight: true });
    expect(stored[4].attrs).toBeUndefined();
    expect(stored[5].attrs).toEqual({ tight: true });
    expect(stored[6].attrs).toBeUndefined();
    expect(stored[7].content![0].marks).toEqual([{ type: 'link', attrs: { href: 'https://a.b', target: '_blank' } }]);
    expect(stored[7].content![1].marks).toEqual([{ type: 'link', attrs: { href: '#a1' } }]);
  });

  it('copies its own formatting through the clipboard form', () => {
    const v = { type: 'orderedList', attrs: { start: 5, tight: true }, content: [{ type: 'listItem', content: [para(text('a'))] }] };
    expect(parse(schema, serialize(schema, v)).content![0]).toEqual(v);
    const p = { type: 'paragraph', attrs: { align: 'justify', id: 'x' }, content: [text('a')] };
    expect(parse(schema, serialize(schema, p)).content![0]).toEqual(p);
  });

  it('aligns, anchors and keeps ids unique', () => {
    let st = stateOf(schema, { type: 'doc', content: [para(text('one')), para(text('two'))] });
    const run = (c: (s: EditorState, d: (t: Transaction) => void) => boolean) => c(st, (tr) => (st = st.apply(tr)));
    st = st.apply(st.tr.setSelection(TextSelection.create(st.doc, 1, 7)));
    expect(run(setAlign('center'))).toBe(true);
    expect(toStored(st.doc).content!.map((n) => n.attrs)).toEqual([{ align: 'center' }, { align: 'center' }]);
    expect(run(setAlign('left'))).toBe(true);
    expect(toStored(st.doc).content!.map((n) => n.attrs)).toEqual([undefined, undefined]);
    st = st.apply(st.tr.setSelection(TextSelection.create(st.doc, 2)));
    expect(run(setAnchor('top'))).toBe(true);
    expect(docIds(st.doc)).toEqual(['top']);
    st = st.apply(st.tr.setSelection(TextSelection.create(st.doc, 7)));
    run(setAnchor('top'));
    expect(docIds(st.doc)).toEqual(['top']);
  });
});
