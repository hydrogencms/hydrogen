import { flushSync, mount } from 'svelte';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import { mounted } from '../../test-setup';
import Editor from './Editor.svelte';

beforeAll(() => {
  const rect = { x: 0, y: 0, top: 0, left: 0, bottom: 0, right: 0, width: 0, height: 0, toJSON: () => ({}) };
  Range.prototype.getBoundingClientRect = () => rect as DOMRect;
  Range.prototype.getClientRects = () => ({ length: 0, item: () => null, [Symbol.iterator]: [][Symbol.iterator] }) as unknown as DOMRectList;
});

const nbsp = ' ';
const cell = (text: string, bold = false) => ({ type: 'tableCell', content: [{ type: 'paragraph', content: [{ type: 'text', text, ...(bold ? { marks: [{ type: 'bold' }] } : {}) }, { type: 'text', text: nbsp }] }] });

// The shape of a migrated landing page: headings and paragraphs holding only a no-break space, a table without
// header cells, a hard break followed by a space. An editor that cannot hold a node of the document shows the
// field as invalid instead of editing it.
const imported = {
  type: 'doc',
  content: [
    { type: 'heading', attrs: { level: 3 }, content: [{ type: 'text', text: 'What makes us different?' }] },
    { type: 'heading', attrs: { level: 4 }, content: [{ type: 'text', text: nbsp }] },
    { type: 'paragraph', content: [{ type: 'text', text: 'Intro' }, { type: 'hardBreak' }, { type: 'text', text: ' ' }] },
    { type: 'table', content: [{ type: 'tableRow', content: [cell('A', true), cell('B', true)] }, { type: 'tableRow', content: [cell('c'), cell('d')] }] },
  ],
};

describe('an imported document', () => {
  it('opens as it is stored: editable, and nothing is emitted', () => {
    const target = document.body.appendChild(document.createElement('div'));
    const onchange = vi.fn();
    mounted.push(mount(Editor, { target, props: { label: 'Content', value: imported, onchange } }));
    flushSync();
    expect(target.querySelector('.h-rte__broken')).toBeNull();
    expect(target.querySelectorAll('.h-rte__doc table tr')).toHaveLength(2);
    expect(onchange).not.toHaveBeenCalled();
  });
});
