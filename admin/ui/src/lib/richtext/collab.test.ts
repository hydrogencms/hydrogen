import { flushSync, mount } from 'svelte';
import { prosemirrorJSONToYXmlFragment, yXmlFragmentToProsemirrorJSON } from 'y-prosemirror';
import type { EditorView } from 'prosemirror-view';
import * as Y from 'yjs';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { COLLAB_CTX } from '../collab/context';
import { mounted } from '../../test-setup';
import Editor from './Editor.svelte';
import { buildSchema } from './schema';

beforeAll(() => {
  const rect = { x: 0, y: 0, top: 0, left: 0, bottom: 0, right: 0, width: 0, height: 0, toJSON: () => ({}) };
  Range.prototype.getBoundingClientRect = () => rect as DOMRect;
  Range.prototype.getClientRects = () => ({ length: 0, item: () => null, [Symbol.iterator]: [][Symbol.iterator] }) as unknown as DOMRectList;
});

const doc = (t: string) => ({ type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: t }] }] });

function setup(frag: Y.XmlFragment | undefined, extra: Record<string, unknown> = {}) {
  const target = document.body.appendChild(document.createElement('div'));
  const onchange = vi.fn();
  const binding = { fragment: (p: string) => (p === 'body' ? frag : undefined) };
  const inst = mount(Editor, { target, props: { label: 'Body', path: 'body', onchange, ...extra }, context: new Map([[COLLAB_CTX, { binding }]]) });
  mounted.push(inst);
  flushSync();
  return { target, onchange, view: () => (inst as unknown as { getView(): EditorView }).getView() };
}

function fragment(text: string) {
  const d = new Y.Doc();
  const f = d.getXmlFragment('f');
  prosemirrorJSONToYXmlFragment(buildSchema({}), doc(text), f);
  return f;
}

describe('RichTextEditor bound to a shared fragment', () => {
  it('shows the fragment, writes edits into it and emits only local changes', async () => {
    const frag = fragment('hello world');
    const { target, onchange, view } = setup(frag);
    await vi.waitFor(() => expect(target.querySelector('.h-rte__doc')?.textContent).toBe('hello world'));
    expect(onchange).not.toHaveBeenCalled();

    const v = view();
    v.dispatch(v.state.tr.insertText('!', 12));
    expect(yXmlFragmentToProsemirrorJSON(frag)).toEqual(doc('hello world!'));
    expect(onchange).toHaveBeenLastCalledWith(doc('hello world!'));

    onchange.mockClear();
    frag.doc!.transact(() => {
      const text = (frag.get(0) as Y.XmlElement).get(0) as Y.XmlText;
      text.insert(0, '>> ');
    }, 'remote');
    flushSync();
    expect(target.querySelector('.h-rte__doc')?.textContent).toBe('>> hello world!');
    expect(onchange).not.toHaveBeenCalled();
  });

  it('undoes only through the shared undo stack', async () => {
    const frag = fragment('abc');
    const { target, view } = setup(frag);
    await vi.waitFor(() => expect(target.querySelector('.h-rte__doc')?.textContent).toBe('abc'));
    const v = view();
    v.dispatch(v.state.tr.insertText('d', 4));
    flushSync();
    expect(target.querySelector('.h-rte__doc')?.textContent).toBe('abcd');
    target.querySelector<HTMLButtonElement>('[aria-label=Undo]')!.click();
    flushSync();
    expect(target.querySelector('.h-rte__doc')?.textContent).toBe('abc');
  });

  it('falls back to the value without a fragment', () => {
    const { target } = setup(undefined, { value: doc('plain') });
    expect(target.querySelector('.h-rte__doc')?.textContent).toBe('plain');
  });
});
