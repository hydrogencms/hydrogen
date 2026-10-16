import { flushSync, mount } from 'svelte';
import { describe, expect, it } from 'vitest';
import { mounted } from '../../test-setup';
import type { Change } from './diff';
import { diffWords } from './seq';
import DiffView from './DiffView.svelte';

const render = (change: Change) => {
  const target = document.body.appendChild(document.createElement('div'));
  mounted.push(mount(DiffView, { target, props: { change } }));
  flushSync();
  return target;
};

describe('DiffView', () => {
  it('marks insertions and deletions with elements and spoken text, not colour alone', () => {
    const el = render({ kind: 'text', segs: diffWords('a quick fox', 'a slow fox') });
    expect(el.querySelector('del')?.textContent).toContain('quick');
    expect(el.querySelector('ins')?.textContent).toContain('slow');
    expect(el.querySelector('del .h-sr')?.textContent).toContain('Deleted');
    expect(el.querySelector('ins .h-sr')?.textContent).toContain('Inserted');
  });
  it('words block statuses', () => {
    const el = render({
      kind: 'blocks',
      same: 2,
      rows: [{ id: '1', type: 'card', label: 'Card', summary: 'Hi', status: 'added', moved: false, props: [], sameProps: [], slots: [], nested: 0 }],
    });
    expect(el.textContent).toContain('Added');
    expect(el.textContent).toContain('Card');
    expect(el.textContent).toContain('2 unchanged blocks');
  });
});
