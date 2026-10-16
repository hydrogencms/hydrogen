import { describe, expect, it } from 'vitest';
import type { Block } from '../blocks/tree';
import { blockPathOf, chainOf, containerOf, formatPath, formPath, isBlockPath, listPath, locate, parsePath } from './paths';

const root: Block[] = [
  { id: 'a', type: 'hero' },
  { id: 'b', type: 'cards', slots: { cards: [{ id: 'c1', type: 'card' }, { id: 'c2', type: 'card', slots: { inner: [{ id: 'd', type: 'x' }] } }] } },
];

describe('paths', () => {
  it('parses and formats round trip', () => {
    for (const p of ['fields/title', 'fields/body', 'fields/body/2', 'fields/body/1/slots/cards/0', 'fields/body/2/slots/cards/0/props/heading', 'fields/body/0/props/a']) {
      const fp = parsePath(p);
      expect(fp, p).not.toBeNull();
      expect(formatPath(fp!)).toBe(p);
    }
    expect(parsePath('fields/body/2/slots/cards/0/props/heading')).toEqual({ field: 'body', chain: [2, 'cards', 0], prop: 'heading' });
  });
  it('rejects invalid paths', () => {
    for (const p of ['', 'fields', 'fields/', 'body/1', 'fields/a/b', 'fields/body/x', 'fields/body/01', 'fields/body/1/slots', 'fields/body/1/slots/cards', 'fields/body/1/props', 'fields/body/props/x', 'fields/body/1/props/a/b', 'fields/body/-1', 'fields/g/sub/x']) {
      expect(parsePath(p), p).toBeNull();
    }
  });
  it('allows a slot named props', () => {
    expect(parsePath('fields/b/1/slots/props/0')).toEqual({ field: 'b', chain: [1, 'props', 0] });
  });
  it('locates blocks', () => {
    expect(locate(root, [1, 'cards', 1])).toMatchObject({ block: { id: 'c2' }, container: { id: 'b', slot: 'cards' }, index: 1 });
    expect(locate(root, [0])).toMatchObject({ block: { id: 'a' }, container: {}, index: 0 });
    expect(locate(root, [1, 'cards', 1, 'inner', 0])?.block.id).toBe('d');
    expect(locate(root, [])).toBeUndefined();
    expect(locate(root, [5])).toBeUndefined();
    expect(locate(root, [1, 'cards'])).toBeUndefined();
    expect(locate(root, [1, 'nope', 0])).toBeUndefined();
  });
  it('finds chains and block paths by id', () => {
    expect(chainOf(root, 'd')).toEqual([1, 'cards', 1, 'inner', 0]);
    expect(chainOf(root, 'zz')).toBeUndefined();
    expect(blockPathOf('body', root, 'c1')).toBe('fields/body/1/slots/cards/0');
    expect(blockPathOf('body', root, 'zz')).toBeUndefined();
  });
  it('maps to form paths', () => {
    expect(formPath({ field: 'title', chain: [] })).toBe('title');
    expect(formPath({ field: 'body', chain: [2] })).toBe('body[2]');
    expect(formPath({ field: 'body', chain: [2, 'cards', 0], prop: 'heading' })).toBe('body[2].slots.cards[0].props.heading');
  });
  it('lists', () => {
    expect(listPath('fields/body/2')).toEqual({ list: 'fields/body', index: 2 });
    expect(listPath('fields/body/2/slots/cards/0')).toEqual({ list: 'fields/body/2/slots/cards', index: 0 });
    expect(listPath('fields/body')).toBeUndefined();
    expect(listPath('fields/body/1/props/x')).toBeUndefined();
    expect(containerOf(root, 'fields/body')).toEqual({ field: 'body', container: {} });
    expect(containerOf(root, 'fields/body/1/slots/cards')).toEqual({ field: 'body', container: { id: 'b', slot: 'cards' } });
    expect(containerOf(root, 'fields/body/1/slots/cards/1/slots/inner')).toEqual({ field: 'body', container: { id: 'c2', slot: 'inner' } });
    expect(containerOf(root, 'fields/body/9/slots/cards')).toBeUndefined();
    expect(containerOf(root, 'fields/body/1')).toBeUndefined();
  });
  it('knows block paths', () => {
    expect(isBlockPath('fields/body/1')).toBe(true);
    expect(isBlockPath('fields/body/1/slots/a/0')).toBe(true);
    expect(isBlockPath('fields/body/1/props/x')).toBe(false);
    expect(isBlockPath('fields/title')).toBe(false);
    expect(isBlockPath('nope')).toBe(false);
  });
});
