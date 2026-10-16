import { describe, expect, it } from 'vitest';
import { dropAt, type LayoutNode } from './drop';

const n = (path: string, x: number, y: number, w: number, h: number, block = 'b'): LayoutNode => ({ path, rect: [x, y, w, h], block });
const all = () => true;

// Three stacked blocks, the second holds a horizontal slot of two cards.
const vertical: LayoutNode[] = [n('fields/body/0', 0, 0, 100, 100), n('fields/body/1', 0, 100, 100, 100), n('fields/body/2', 0, 200, 100, 100), { path: 'fields/title', rect: [0, 0, 50, 10] }];
const nested: LayoutNode[] = [
  n('fields/body/0', 0, 0, 200, 100),
  n('fields/body/1', 0, 100, 200, 100),
  n('fields/body/1/slots/cards/0', 0, 100, 100, 100),
  n('fields/body/1/slots/cards/1', 100, 100, 100, 100),
];

describe('dropAt', () => {
  it('vertical list: before / after by half', () => {
    expect(dropAt(vertical, 'fields/body/2', 50, 110, all)).toEqual({ list: 'fields/body', index: 1, line: [0, 99, 100, 2] });
    expect(dropAt(vertical, 'fields/body/0', 50, 20, all)).toBeNull();
  });
  it('vertical: dragging 0 below 1', () => {
    expect(dropAt(vertical, 'fields/body/0', 50, 190, all)).toEqual({ list: 'fields/body', index: 2, line: [0, 199, 100, 2] });
  });
  it('no-op positions give null', () => {
    expect(dropAt(vertical, 'fields/body/1', 50, 110, all)).toBeNull();
    expect(dropAt(vertical, 'fields/body/1', 50, 190, all)).toBeNull();
  });
  it('ignores non-block nodes and the dragged node', () => {
    expect(dropAt(vertical, 'fields/body/2', 5, 5, all)?.list).toBe('fields/body');
  });
  it('horizontal list', () => {
    const d = dropAt(nested, 'fields/body/0', 30, 150, all);
    expect(d).toEqual({ list: 'fields/body/1/slots/cards', index: 0, line: [-1, 100, 2, 100] });
    expect(dropAt(nested, 'fields/body/0', 190, 150, all)).toEqual({ list: 'fields/body/1/slots/cards', index: 2, line: [199, 100, 2, 100] });
  });
  it('horizontal within the same list: no-op next to itself', () => {
    expect(dropAt(nested, 'fields/body/1/slots/cards/0', 120, 150, all)).toBeNull();
    expect(dropAt(nested, 'fields/body/1/slots/cards/0', 190, 150, all)?.index).toBe(2);
  });
  it('disallowed slot falls back to the ancestor list', () => {
    // upper half of the card: before body/1, which is where body/0 already ends up
    expect(dropAt(nested, 'fields/body/0', 30, 120, (l) => l === 'fields/body')).toBeNull();
    expect(dropAt(nested, 'fields/body/0', 30, 190, (l) => l === 'fields/body')).toEqual({ list: 'fields/body', index: 2, line: [0, 199, 200, 2] });
  });
  it('skips the dragged node and its descendants', () => {
    // pointer over a child of the dragged block: the dragged block itself is not a candidate
    const d = dropAt(nested, 'fields/body/1', 30, 150, all);
    expect(d).toBeNull();
    const e = dropAt(nested, 'fields/body/1', 30, 20, all);
    expect(e).toEqual({ list: 'fields/body', index: 0, line: [0, -1, 200, 2] });
  });
  it('null when nothing is allowed or there are no blocks', () => {
    expect(dropAt(vertical, 'fields/body/0', 50, 250, () => false)).toBeNull();
    expect(dropAt([], 'fields/body/0', 1, 1, all)).toBeNull();
  });
  it('outside every node: nearest', () => {
    expect(dropAt(vertical, 'fields/body/0', 50, 400, all)).toEqual({ list: 'fields/body', index: 3, line: [0, 299, 100, 2] });
  });
});
