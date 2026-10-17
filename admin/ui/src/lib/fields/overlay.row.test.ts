import { flushSync, mount, unmount } from 'svelte';
import { afterEach, describe, expect, it } from 'vitest';
import type { CompiledField } from '../types.svelte';
import Host from './FieldRowOverlayHost.test.svelte';

const field = { name: 'title', label: 'Title', type: 'string', shape: { base: 'string' }, translatable: true } as CompiledField;
let app: ReturnType<typeof mount> | undefined;
afterEach(() => {
  if (app) unmount(app);
  app = undefined;
  document.body.innerHTML = '';
});
const btn = (name: string) => [...document.querySelectorAll('button')].find((b) => b.textContent?.trim() === name) as HTMLButtonElement | undefined;

describe('overlay field row', () => {
  it('shows an inherited field read-only, then overrides it and resets it', () => {
    app = mount(Host, { target: document.body, props: { fields: {}, field } });
    flushSync();
    expect(document.body.textContent).toContain('Inherited from de');
    const input = document.querySelector('input') as HTMLInputElement;
    expect(input.disabled || input.readOnly).toBe(true);
    expect(input.value).toBe('Hallo');
    expect(document.querySelector('output')!.textContent).toBe('{}');
    btn('Override')!.click();
    flushSync();
    expect(document.body.textContent).toContain('Overridden');
    expect((document.querySelector('input') as HTMLInputElement).disabled).toBe(false);
    expect(document.querySelector('output')!.textContent).toBe('{"title":"Hallo"}');
    btn('Reset to de')!.click();
    flushSync();
    expect(document.body.textContent).toContain('Inherited from de');
    expect(document.querySelector('output')!.textContent).toBe('{}');
  });
  it('starts overridden when the form holds the key', () => {
    app = mount(Host, { target: document.body, props: { fields: { title: 'Servus' }, field } });
    flushSync();
    expect(document.body.textContent).toContain('Overridden');
    expect((document.querySelector('input') as HTMLInputElement).value).toBe('Servus');
    expect(btn('Reset to de')).toBeTruthy();
  });
});
