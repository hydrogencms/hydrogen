import { describe, expect, it } from 'vitest';
import {
  canInsert,
  canMove,
  canRemove,
  cleanBlocks,
  cloneBlock,
  find,
  hydrateBlocks,
  insertAt,
  listAt,
  moveTo,
  newBlock,
  removeById,
  reorder,
  routeErrors,
  ruleOfField,
  schemaOf,
  slotDepth,
  summary,
  targets,
  uuidv7,
  type Block,
  type BlockType,
} from './tree';
import type { CompiledField } from '../types.svelte';

const f = (name: string, base = 'string', extra: Partial<CompiledField> = {}): CompiledField => ({ name, label: name, type: base, shape: { base }, ...extra });
const defs: BlockType[] = [
  { name: 'hero', label: 'Hero', props: [f('heading'), f('tone', 'string', { shape: { base: 'string', format: 'token', enum: ['light', 'dark'] }, default: 'light' })] },
  { name: 'richText', label: 'Rich text', props: [f('body', 'richtext')] },
  { name: 'cards', label: 'Cards', props: [f('heading')], slots: [{ name: 'items', label: 'Cards', allow: ['card'], min: 1, max: 2 }] },
  { name: 'card', label: 'Card', props: [f('title')] },
  { name: 'shared', label: 'Shared block', props: [f('item', 'ref')] },
];
const schema = schemaOf(defs, { allow: ['hero', 'richText', 'cards', 'shared'], max: 4 });
let n = 0;
const id = () => `id${++n}`;
const hero = (i: string, heading = i): Block => ({ id: i, type: 'hero', props: { heading } });
const card = (i: string, title = i): Block => ({ id: i, type: 'card', props: { title } });
const cards = (i: string, ...kids: Block[]): Block => ({ id: i, type: 'cards', props: {}, slots: { items: kids } });

describe('ids', () => {
  it('makes UUIDv7 strings that sort by time', () => {
    const a = uuidv7(1_700_000_000_000);
    const b = uuidv7(1_700_000_000_001);
    expect(a).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(a < b).toBe(true);
  });
});

describe('insert / remove / find', () => {
  it('inserts into the root and into a slot', () => {
    let t = [hero('h'), cards('c')];
    t = insertAt(t, {}, 1, hero('x'));
    expect(t.map((b) => b.id)).toEqual(['h', 'x', 'c']);
    t = insertAt(t, { id: 'c', slot: 'items' }, 0, card('k1'));
    expect(listAt(t, { id: 'c', slot: 'items' })!.map((b) => b.id)).toEqual(['k1']);
    expect(find(t, 'k1')!.container).toEqual({ id: 'c', slot: 'items' });
  });

  it('does not mutate its input and shares untouched subtrees', () => {
    const t = [hero('h'), cards('c', card('k'))];
    const u = removeById(t, 'h');
    expect(t).toHaveLength(2);
    expect(u[0]).toBe(t[1]);
    const v = insertAt(t, { id: 'c', slot: 'items' }, 1, card('k2'));
    expect(v[0]).toBe(t[0]);
  });

  it('removes nested blocks', () => {
    const t = removeById([cards('c', card('a'), card('b'))], 'a');
    expect(listAt(t, { id: 'c', slot: 'items' })!.map((b) => b.id)).toEqual(['b']);
  });
});

describe('move and reorder', () => {
  it('reorders within a list and stops at the ends', () => {
    const t = [hero('a'), hero('b'), hero('c')];
    expect(reorder(t, 'b', -1)!.map((b) => b.id)).toEqual(['b', 'a', 'c']);
    expect(reorder(t, 'b', 1)!.map((b) => b.id)).toEqual(['a', 'c', 'b']);
    expect(reorder(t, 'a', -1)).toBeUndefined();
    expect(reorder(t, 'c', 1)).toBeUndefined();
  });

  it('moves down to a later position (index counts before the move)', () => {
    const t = [hero('a'), hero('b'), hero('c')];
    expect(moveTo(t, 'a', {}, 3)!.map((b) => b.id)).toEqual(['b', 'c', 'a']);
    expect(moveTo(t, 'a', {}, 2)!.map((b) => b.id)).toEqual(['b', 'a', 'c']);
    expect(moveTo(t, 'a', {}, 1)).toBe(t);
  });

  it('moves between slots and out to the root', () => {
    const t = [cards('c1', card('a'), card('b')), cards('c2')];
    const m = moveTo(t, 'a', { id: 'c2', slot: 'items' }, 0)!;
    expect(listAt(m, { id: 'c1', slot: 'items' })!.map((b) => b.id)).toEqual(['b']);
    expect(listAt(m, { id: 'c2', slot: 'items' })!.map((b) => b.id)).toEqual(['a']);
  });

  it('refuses to move a block into itself or its descendants', () => {
    const t = [cards('c', card('a'))];
    expect(moveTo(t, 'c', { id: 'c', slot: 'items' }, 0)).toBeUndefined();
    expect(canMove(schema, t, 'c', { id: 'c', slot: 'items' })).toEqual({ ok: false, reason: 'self' });
  });
});

describe('rules', () => {
  const t = [hero('h1'), hero('h2'), hero('h3'), cards('c', card('k1'), card('k2'))];

  it('enforces allow', () => {
    expect(canInsert(schema, t, {}, ['card'])).toEqual({ ok: false, reason: 'type' });
    expect(canInsert(schema, t.slice(0, 2), {}, ['hero']).ok).toBe(true);
  });

  it('enforces max', () => {
    expect(canInsert(schema, t, {}, ['hero'])).toEqual({ ok: false, reason: 'max', limit: 4 });
    expect(canInsert(schema, t, { id: 'c', slot: 'items' }, ['card'])).toEqual({ ok: false, reason: 'max', limit: 2 });
  });

  it('enforces min on removal and on moving out', () => {
    const one = [cards('c', card('k'))];
    expect(canRemove(schema, one, 'k')).toEqual({ ok: false, reason: 'min', limit: 1 });
    expect(canRemove(schema, t, 'k1').ok).toBe(true);
    expect(canMove(schema, [cards('c', card('k')), cards('d')], 'k', { id: 'd', slot: 'items' })).toEqual({ ok: false, reason: 'min', limit: 1 });
  });

  it('allows a move inside a full list', () => {
    expect(canMove(schema, t, 'k1', { id: 'c', slot: 'items' }).ok).toBe(true);
  });

  it('lists move targets with trails', () => {
    const tree = [hero('h'), cards('c', card('a'), card('b')), cards('d')];
    const to = targets(schema, tree, 'a', 'Body');
    expect(to.map((x) => x.trail.join(' > '))).toEqual(['Body > Cards 2 > Cards', 'Body > Cards 3 > Cards']);
    const toHero = targets(schema, tree, 'h', 'Body');
    expect(toHero.map((x) => x.trail.join(' > '))).toEqual(['Body']);
  });
});

describe('copy', () => {
  it('clones with new ids at every level and keeps the content', () => {
    n = 0;
    const src = cards('c', card('a', 'A'));
    const c = cloneBlock(src, id);
    expect(c.id).toBe('id1');
    expect(c.slots!.items![0]!.id).toBe('id2');
    expect(c.slots!.items![0]!.props).toEqual({ title: 'A' });
    expect(src.id).toBe('c');
  });

  it('works on reactive proxies (plain JSON copy)', () => {
    const p = new Proxy(hero('h'), {});
    expect(cloneBlock(p, () => 'z').props).toEqual({ heading: 'h' });
  });

  it('a new block carries the defaults of its type', () => {
    const b = newBlock(defs[0]!, () => 'new');
    expect(b).toEqual({ id: 'new', type: 'hero', props: { tone: 'light' } });
  });
});

describe('summary', () => {
  it('uses the first filled text prop, skipping tokens', () => {
    expect(summary(hero('h', 'Hello   world'), defs[0])).toBe('Hello world');
    expect(summary({ id: 'x', type: 'hero', props: { tone: 'dark' } }, defs[0])).toBe('');
  });
  it('reads rich text', () => {
    const body = { type: 'doc', content: [{ type: 'paragraph', content: [{ type: 'text', text: 'Rich' }, { type: 'text', text: ' words' }] }] };
    expect(summary({ id: 'x', type: 'richText', props: { body } }, defs[1])).toBe('Rich words');
  });
  it('truncates', () => {
    expect(summary(hero('h', 'x'.repeat(200)), defs[0], 20)).toHaveLength(20);
  });
});

describe('saving', () => {
  it('drops empty props and empty slots, keeps a props object', () => {
    const out = cleanBlocks([{ id: 'a', type: 'cards', props: { heading: '', n: 0, off: false, x: null }, slots: { items: [], other: [{ id: 'b', type: 'card', props: { title: '' } }] } }]) as Block[];
    expect(out).toEqual([{ id: 'a', type: 'cards', props: { n: 0, off: false }, slots: { other: [{ id: 'b', type: 'card', props: {} }] } }]);
    expect((cleanBlocks([{ id: 'a', type: 'x', props: {}, slots: { s: [] } }]) as Block[])[0]).not.toHaveProperty('slots');
  });
  it('hydrates missing props objects, recursively', () => {
    const out = hydrateBlocks([{ id: 'a', type: 'cards', slots: { items: [{ id: 'b', type: 'card' }] } }]) as Block[];
    expect(out[0]!.props).toEqual({});
    expect(out[0]!.slots!.items![0]!.props).toEqual({});
  });
  it('clean(hydrate(x)) equals clean(x)', () => {
    const x = [{ id: 'a', type: 'cards', slots: { items: [{ id: 'b', type: 'card' }] } }];
    expect(cleanBlocks(hydrateBlocks(x))).toEqual(cleanBlocks(x));
  });
});

describe('rules from a field', () => {
  it('reads allow from the shape and limits from the options', () => {
    const field = f('body', 'blocks', { shape: { base: 'blocks', allow: ['hero'] }, options: { max: 5, min: 1 } });
    expect(ruleOfField(field)).toEqual({ allow: ['hero'], min: 1, max: 5 });
  });
});

describe('error routing', () => {
  const tree = [hero('h'), cards('c', card('k1'), card('k2'))];
  it('leaves prop errors to their widgets and counts them on the block', () => {
    const r = routeErrors(schema, tree, 'body', { 'body[0].props.heading': 'required', 'body[1].slots.items[1].props.title': 'required' });
    expect(r.block.size).toBe(0);
    expect(r.count.get('h')).toBe(1);
    expect(r.count.get('c')).toBe(1);
    expect(r.count.get('k2')).toBe(1);
    expect(r.orphan).toEqual([]);
  });
  it('puts slot, block and field errors where they belong', () => {
    const r = routeErrors(schema, tree, 'body', {
      body: 'too many',
      'body[1].slots.items': 'needs at least 1',
      'body[0]': 'type not allowed',
      'body[0].props.nope': 'unknown prop',
    });
    expect(r.root).toEqual(['too many']);
    expect(r.slot.get('c/items')).toEqual(['needs at least 1']);
    expect(r.block.get('h')).toEqual([
      ['body[0]', 'type not allowed'],
      ['body[0].props.nope', 'unknown prop'],
    ]);
  });
  it('reports paths with no block as orphans', () => {
    const r = routeErrors(schema, tree, 'body', { 'body[9].props.x': 'bad', 'body[1].slots.items[7].props.title': 'bad', 'other[0]': 'ignored' });
    expect(r.orphan.map(([p]) => p)).toEqual(['body[9].props.x', 'body[1].slots.items[7].props.title']);
  });
});

describe('server limits', () => {
  const nestSchema = schemaOf(
    [{ name: 'box', label: 'Box', props: [], slots: [{ name: 'items', label: 'Items', allow: ['box'] }] }],
    { allow: ['box'] },
  );
  const box = (id: string, ...kids: Block[]): Block => ({ id, type: 'box', props: {}, slots: { items: kids } });

  it('refuses nesting deeper than 8 slot levels', () => {
    let tree: Block[] = [box('b0')];
    let parent = 'b0';
    for (let i = 1; i <= 8; i++) {
      expect(canInsert(nestSchema, tree, { id: parent, slot: 'items' }, ['box']).ok).toBe(true);
      tree = insertAt(tree, { id: parent, slot: 'items' }, 0, box('b' + i));
      parent = 'b' + i;
    }
    expect(canInsert(nestSchema, tree, { id: parent, slot: 'items' }, ['box'])).toEqual({ ok: false, reason: 'depth', limit: 8 });
  });

  it('counts the depth of the block being pasted or moved', () => {
    const tree = [box('a', box('b', box('c'))), box('d', box('e', box('f', box('g', box('h', box('i', box('j')))))))];
    const sub = tree[0]!;
    expect(slotDepth(sub)).toBe(2);
    expect(canInsert(nestSchema, tree, { id: 'g', slot: 'items' }, ['box'], 0, [sub]).ok).toBe(true);
    expect(canInsert(nestSchema, tree, { id: 'j', slot: 'items' }, ['box'], 0, [sub])).toEqual({ ok: false, reason: 'depth', limit: 8 });
    expect(canMove(nestSchema, tree, 'a', { id: 'j', slot: 'items' }).ok).toBe(false);
  });

  it('refuses more than 1000 blocks, counting a pasted subtree', () => {
    const many = Array.from({ length: 999 }, (_, i) => box('x' + i));
    expect(canInsert(nestSchema, many, {}, ['box']).ok).toBe(true);
    expect(canInsert(nestSchema, [...many, box('y')], {}, ['box'])).toEqual({ ok: false, reason: 'count', limit: 1000 });
    expect(canInsert(nestSchema, many, {}, ['box'], 0, [box('p', box('q'))])).toEqual({ ok: false, reason: 'count', limit: 1000 });
  });
});
