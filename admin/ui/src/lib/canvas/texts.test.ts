import { describe, expect, it } from 'vitest';
import type { CompiledField } from '../types.svelte';
import type { BlockType } from '../blocks/tree';
import { editableText, textRefs } from './texts';

const f = (name: string, type: string, extra: Partial<CompiledField> = {}): CompiledField => ({ name, label: name.toUpperCase(), type, shape: { base: type === 'blocks' ? 'blocks' : 'string' } as never, ...extra });
const fields = [f('title', 'string'), f('summary', 'text'), f('secret', 'string', { admin: { hidden: true } }), f('ro', 'string', { admin: { readonly: true } }), f('n', 'number'), f('body', 'blocks')];
const types: Record<string, BlockType> = {
  hero: { name: 'hero', label: 'Hero', props: [f('heading', 'string'), f('lead', 'text'), f('img', 'asset'), f('hid', 'string', { admin: { hidden: true } }), f('ro', 'string', { admin: { readonly: true } })] },
  cards: { name: 'cards', label: 'Cards', props: [f('title', 'string')], slots: [{ name: 'cards', label: 'Cards', allow: ['card'] }] },
  card: { name: 'card', label: 'Card', props: [f('heading', 'string')], slots: [{ name: 'inner', label: 'Inner', allow: ['card'] }] },
  shared: { name: 'shared', label: 'Shared', props: [f('item', 'string')] },
};
const look = (n: string) => types[n];
const doc = {
  title: 'Home',
  summary: 'Hello\nworld',
  secret: 'x',
  ro: 'R',
  n: 3,
  body: [
    { id: 'b0', type: 'hero', props: { heading: 'Welcome', lead: 'Lead', img: 'a', hid: 'h' } },
    { id: 'b1', type: 'cards', props: { title: 'Cards' }, slots: { cards: [{ id: 'c0', type: 'card', props: { heading: 'One' }, slots: { inner: [{ id: 'c1', type: 'card', props: { heading: 'Deep' } }] } }] } },
    { id: 'b2', type: 'shared', props: { item: 'abc' }, blocks: [{ id: 'z', type: 'hero', props: { heading: 'Inlined' } }] },
    { id: 'b3', type: 'hero', props: { heading: '' } },
  ],
};

describe('textRefs', () => {
  const refs = textRefs(fields, doc, look);
  it('maps top-level and block props, sorted, with scopes', () => {
    expect(refs).toEqual([
      { path: 'fields/body/0/props/heading', text: 'Welcome', scope: 'fields/body/0' },
      { path: 'fields/body/0/props/lead', text: 'Lead', scope: 'fields/body/0' },
      { path: 'fields/body/1/props/title', text: 'Cards', scope: 'fields/body/1' },
      { path: 'fields/body/1/slots/cards/0/props/heading', text: 'One', scope: 'fields/body/1/slots/cards/0' },
      { path: 'fields/body/1/slots/cards/0/slots/inner/0/props/heading', text: 'Deep', scope: 'fields/body/1/slots/cards/0/slots/inner/0' },
      { path: 'fields/ro', text: 'R', scope: '' },
      { path: 'fields/summary', text: 'Hello\nworld', scope: '' },
      { path: 'fields/title', text: 'Home', scope: '' },
    ]);
  });
  it('excludes shared inlined blocks, hidden, empty and long values', () => {
    expect(refs.some((r) => r.text === 'Inlined' || r.text === 'x' || r.text === 'h')).toBe(false);
    expect(textRefs(fields, { title: 'x'.repeat(501) }, look)).toEqual([]);
  });
  it('is deterministic', () => {
    expect(textRefs(fields, doc, look)).toEqual(refs);
    const paths = refs.map((r) => r.path);
    expect(paths).toEqual([...paths].sort());
  });
});

describe('editableText', () => {
  const e = (p: string) => editableText(fields, look, doc, p);
  it('classifies', () => {
    expect(e('fields/title')).toEqual({ multiline: false, label: 'TITLE' });
    expect(e('fields/summary')).toEqual({ multiline: true, label: 'SUMMARY' });
    expect(e('fields/body/0/props/heading')).toEqual({ multiline: false, label: 'HEADING' });
    expect(e('fields/body/0/props/lead')?.multiline).toBe(true);
    expect(e('fields/body/1/slots/cards/0/slots/inner/0/props/heading')).not.toBeNull();
  });
  it('refuses others', () => {
    for (const p of ['fields/secret', 'fields/ro', 'fields/n', 'fields/body', 'fields/body/0', 'fields/body/0/props/img', 'fields/body/0/props/hid', 'fields/body/0/props/ro', 'fields/body/0/props/nope', 'fields/body/2/props/item', 'fields/body/9/props/heading', 'fields/nope', 'junk']) {
      expect(e(p), p).toBeNull();
    }
  });
});
