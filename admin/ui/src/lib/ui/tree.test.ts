import { flushSync, mount } from 'svelte';
import { describe, expect, it, vi } from 'vitest';
import Tree from './Tree.svelte';
import { dropPosition, flatten, isDescendant, type TreeNode } from './tree';
import { mounted } from '../../test-setup';

const data: TreeNode[] = [
  { id: 'a', label: 'A', children: [{ id: 'a1', label: 'A1' }, { id: 'a2', label: 'A2' }] },
  { id: 'b', label: 'B', hasChildren: true },
  { id: 'c', label: 'C' },
];

describe('flatten', () => {
  it('lists visible rows with levels and set info', () => {
    const f = flatten(data, new Set(['a']), (n) => n.children);
    expect(f.map((x) => [x.node.id, x.level, x.pos, x.size])).toEqual([
      ['a', 1, 1, 3], ['a1', 2, 1, 2], ['a2', 2, 2, 2], ['b', 1, 2, 3], ['c', 1, 3, 3],
    ]);
    expect(f[0].open).toBe(true);
    expect(f.find((x) => x.node.id === 'b')!.expandable).toBe(true);
    expect(f.find((x) => x.node.id === 'c')!.expandable).toBe(false);
    expect(isDescendant(f, 'a1', 'a')).toBe(true);
    expect(isDescendant(f, 'b', 'a')).toBe(false);
  });
  it('picks a drop position from the pointer height', () => {
    const r = { top: 100, height: 40 };
    expect([105, 120, 135].map((y) => dropPosition(y, r))).toEqual(['before', 'inside', 'after']);
  });
});

function setup(props: Record<string, unknown> = {}) {
  const target = document.body.appendChild(document.createElement('div'));
  mounted.push(mount(Tree, { target, props: { nodes: data, label: 'Pages', ...props } }));
  const items = () => [...target.querySelectorAll<HTMLElement>('[role=treeitem]')];
  const key = (el: Element, k: string) => {
    el.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
    flushSync();
  };
  return { target, items, key };
}

describe('Tree', () => {
  it('exposes tree roles and a single tab stop', () => {
    const { target, items } = setup();
    expect(target.querySelector('[role=tree]')?.getAttribute('aria-label')).toBe('Pages');
    expect(items().map((i) => i.tabIndex)).toEqual([0, -1, -1]);
    expect(items()[0].getAttribute('aria-expanded')).toBe('false');
  });

  it('arrow keys expand, collapse and move focus', async () => {
    const { items, key } = setup();
    items()[0].focus();
    key(items()[0], 'ArrowRight');
    await vi.waitFor(() => expect(items().length).toBe(5));
    expect(items()[0].getAttribute('aria-expanded')).toBe('true');
    key(items()[0], 'ArrowRight');
    await vi.waitFor(() => expect(document.activeElement).toBe(items()[1]));
    key(items()[1], 'ArrowDown');
    await vi.waitFor(() => expect(document.activeElement).toBe(items()[2]));
    key(items()[2], 'ArrowLeft');
    await vi.waitFor(() => expect(document.activeElement).toBe(items()[0]));
    key(items()[0], 'ArrowLeft');
    expect(items().length).toBe(3);
    key(items()[0], 'End');
    await vi.waitFor(() => expect(document.activeElement).toBe(items()[2]));
  });

  it('loads children lazily, Enter activates, Space selects, m requests a move', async () => {
    const loadChildren = vi.fn(async () => [{ id: 'b1', label: 'B1' }]);
    const onactivate = vi.fn();
    const onselect = vi.fn();
    const onmoverequest = vi.fn();
    const { items, key } = setup({ loadChildren, onactivate, onselect, onmoverequest });
    key(items()[1], 'ArrowRight');
    await vi.waitFor(() => expect(items().length).toBe(4));
    expect(loadChildren).toHaveBeenCalledTimes(1);
    key(items()[1], 'Enter');
    expect(onactivate).toHaveBeenCalledWith(expect.objectContaining({ id: 'b' }));
    key(items()[1], ' ');
    expect(onselect).toHaveBeenCalledWith(expect.objectContaining({ id: 'b' }));
    key(items()[2], 'm');
    expect(onmoverequest).toHaveBeenCalledWith(expect.objectContaining({ id: 'b1' }));
  });
});
