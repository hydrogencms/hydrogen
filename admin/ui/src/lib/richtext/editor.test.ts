import { flushSync, mount } from 'svelte';
import { TextSelection } from 'prosemirror-state';
import type { EditorView } from 'prosemirror-view';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { mounted } from '../../test-setup';
import Editor from './Editor.svelte';
import RichTextEditor from './RichTextEditor.svelte';
import { reactive } from './testing.svelte';
import type { RichTextProps } from './types';

beforeAll(() => {
  // jsdom has no layout; ProseMirror asks for rects when it scrolls a selection into view.
  const rect = { x: 0, y: 0, top: 0, left: 0, bottom: 0, right: 0, width: 0, height: 0, toJSON: () => ({}) };
  Range.prototype.getBoundingClientRect = () => rect as DOMRect;
  Range.prototype.getClientRects = () => ({ length: 0, item: () => null, [Symbol.iterator]: [][Symbol.iterator] }) as unknown as DOMRectList;
});

const doc = { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'hello world' }] }] };

function setup(props: Partial<RichTextProps> = {}, Comp: typeof Editor = Editor) {
  const target = document.body.appendChild(document.createElement('div'));
  const onchange = vi.fn();
  const inst = mount(Comp, { target, props: { label: 'Body', onchange, ...props } });
  mounted.push(inst);
  flushSync();
  const dom = target.querySelector<HTMLElement>('.h-rte__doc')!;
  const view = (inst as unknown as { getView(): EditorView }).getView();
  return { target, onchange, dom, view };
}
const label = (target: HTMLElement) => [...target.querySelectorAll('[role=toolbar] button')].map((b) => b.getAttribute('aria-label') ?? b.textContent?.trim());

describe('RichTextEditor', () => {
  it('is an accessible textbox with a labelled toolbar', () => {
    const { target, dom } = setup({ value: doc, error: 'Required' });
    expect(dom.getAttribute('role')).toBe('textbox');
    expect(dom.getAttribute('aria-multiline')).toBe('true');
    expect(dom.getAttribute('aria-label')).toBe('Body');
    expect(dom.getAttribute('aria-invalid')).toBe('true');
    expect(dom.textContent).toBe('hello world');
    expect(target.querySelector('[role=toolbar]')?.getAttribute('aria-label')).toBe('Formatting');
  });

  it('roves one tab stop through the toolbar', () => {
    const { target } = setup({ value: doc });
    const buttons = [...target.querySelectorAll<HTMLButtonElement>('[role=toolbar] button')].filter((b) => !b.disabled);
    expect(buttons.filter((b) => b.tabIndex === 0)).toHaveLength(1);
    expect(buttons.filter((b) => b.tabIndex === -1).length).toBe(buttons.length - 1);
    buttons[0].focus();
    buttons[0].dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    flushSync();
    expect(document.activeElement).toBe(buttons[1]);
    expect(buttons[1].tabIndex).toBe(0);
    expect(buttons[0].tabIndex).toBe(-1);
  });

  it('shows only the controls the field allows', () => {
    const all = label(setup({ value: doc }).target);
    for (const name of ['Bold', 'Italic', 'Inline code', 'Link', 'Bulleted list', 'Numbered list', 'Quote', 'Code block', 'Horizontal rule', 'Undo', 'Redo']) {
      expect(all).toContain(name);
    }
    document.body.innerHTML = '';
    const few = label(setup({ value: doc, config: { marks: ['bold'], nodes: ['paragraph', 'bulletList'] } }).target);
    expect(few).toContain('Bold');
    expect(few).toContain('Bulleted list');
    for (const name of ['Italic', 'Inline code', 'Link', 'Numbered list', 'Quote', 'Code block', 'Horizontal rule']) expect(few).not.toContain(name);
  });

  it('emits the stored document on edits and undefined when emptied', () => {
    const { onchange, view } = setup({ value: doc });
    view.dispatch(view.state.tr.insertText('!', 12));
    expect(onchange).toHaveBeenLastCalledWith({ type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'hello world!' }] }] });
    view.dispatch(view.state.tr.delete(1, view.state.doc.content.size - 1));
    expect(onchange).toHaveBeenLastCalledWith(undefined);
  });

  it('applies formatting from the toolbar', () => {
    const { target, onchange, view } = setup({ value: doc });
    view.dispatch(view.state.tr.setSelection(TextSelection.create(view.state.doc, 1, 6)));
    flushSync();
    target.querySelector<HTMLButtonElement>('[aria-label=Bold]')!.click();
    flushSync();
    expect(onchange).toHaveBeenLastCalledWith({
      type: 'doc',
      content: [{ type: 'paragraph', content: [{ type: 'text', text: 'hello', marks: [{ type: 'bold' }] }, { type: 'text', text: ' world' }] }],
    });
    expect(target.querySelector('[aria-label=Bold]')!.getAttribute('aria-pressed')).toBe('true');
  });

  it('follows outside changes of the value', () => {
    const target = document.body.appendChild(document.createElement('div'));
    const props = reactive<RichTextProps>({ label: 'Body', value: doc });
    mounted.push(mount(Editor, { target, props }));
    flushSync();
    const dom = target.querySelector<HTMLElement>('.h-rte__doc')!;
    expect(dom.textContent).toBe('hello world');
    props.value = { type: 'doc', content: [{ type: 'heading', attrs: { level: 3 }, content: [{ type: 'text', text: 'other locale' }] }] };
    flushSync();
    expect(dom.querySelector('h3')?.textContent).toBe('other locale');
  });

  it('reports a document it cannot show', () => {
    const bad = { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'x', marks: [{ type: 'italic' }] }] }] };
    const { target, onchange } = setup({ value: bad, config: { marks: ['bold'] } });
    expect(target.querySelector('.h-rte__broken')?.textContent).toContain('cannot be shown');
    expect(onchange).not.toHaveBeenCalled();
  });

  it('is read only without a toolbar', () => {
    const { target, dom } = setup({ value: doc, readonly: true });
    expect(target.querySelector('[role=toolbar]')).toBeNull();
    expect(dom.getAttribute('contenteditable')).toBe('false');
    expect(dom.getAttribute('aria-readonly')).toBe('true');
  });

  it('loads lazily behind a skeleton', async () => {
    const target = document.body.appendChild(document.createElement('div'));
    mounted.push(mount(RichTextEditor, { target, props: { label: 'Body', value: doc } }));
    flushSync();
    expect(target.querySelector('.h-rte-skel')).not.toBeNull();
    await vi.waitFor(() => expect(target.querySelector('.h-rte__doc')).not.toBeNull());
    expect(target.querySelector('.h-rte-skel')).toBeNull();
  });
});
