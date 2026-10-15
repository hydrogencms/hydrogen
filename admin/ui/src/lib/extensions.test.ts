import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { flushSync, mount, tick } from 'svelte';
import { mounted } from '../test-setup';
import { session } from './auth/session.svelte';
import { setConfig } from './config';
import { ext, navigateTo, parseExtensions, safePath } from './extensions.svelte';
import { getCtx, mountElement } from './ext/mount';
import { Form } from './form.svelte';
import { t } from './i18n';
import { router } from './router.svelte';
import { toast } from './ui/toast.svelte';
import type { CompiledField } from './types.svelte';
import ViewHost from './ext/ViewHost.svelte';
import FieldRowHost from './fields/FieldRowHost.test.svelte';

const reply = (body: unknown, ok = true) => Promise.resolve({ ok, status: ok ? 200 : 404, json: async () => body }) as unknown as Promise<Response>;

beforeEach(async () => {
  toast.items = [];
  await ext.boot({});
});
afterEach(() => {
  vi.restoreAllMocks();
  document.head.querySelectorAll('link').forEach((l) => l.remove());
  document.documentElement.style.removeProperty('--h-accent');
});

describe('manifest', () => {
  it('defaults to empty lists', () => {
    const m = parseExtensions(undefined);
    expect(m).toMatchObject({ elements: [], strings: [], panels: [], actions: [], dashboard: [], views: [], slots: [], widgets: {}, cells: {}, theme: {} });
    expect(parseExtensions({ strings: '/a.json', panels: 'x' }).strings).toEqual(['/a.json']);
    expect(parseExtensions({ panels: 'x' }).panels).toEqual([]);
  });
});

describe('theme and strings', () => {
  it('applies name, accent and css', async () => {
    await ext.boot({ theme: { name: 'Acme CMS', accent: '#0b6', css: '/admin/x/theme.css', logo: '/admin/x/logo.svg' } });
    expect(t('app.name')).toBe('Acme CMS');
    expect(document.documentElement.style.getPropertyValue('--h-accent')).toBe('#0b6');
    expect(document.head.querySelector('link[rel=stylesheet]')?.getAttribute('href')).toBe('/admin/x/theme.css');
    expect(ext.logo).toBe('/admin/x/logo.svg');
  });

  it('merges strings in listed order, later wins, and ignores failures with a warning', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    ext.fetcher = vi.fn((u: RequestInfo | URL) => {
      if (String(u) === '/p.json') return reply({ 'x.a': 'project', 'x.b': 'project' });
      if (String(u) === '/m.json') return reply({ 'x.b': 'module' });
      return reply({}, false);
    }) as unknown as typeof fetch;
    await ext.boot({ strings: ['/p.json', '/m.json', '/gone.json'] });
    expect(t('x.a')).toBe('project');
    expect(t('x.b')).toBe('module');
    expect(warn).toHaveBeenCalledTimes(1);
  });
});

describe('ensureElements', () => {
  it('imports each url once and tolerates a failing one', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    const seen: string[] = [];
    ext.importer = async (u) => {
      seen.push(u);
      if (u === '/bad.js') throw new Error('boom');
    };
    await ext.boot({ elements: ['/a.js', '/bad.js', '/b.js'] });
    await Promise.all([ext.ensureElements(), ext.ensureElements()]);
    expect(seen.filter((u) => u !== '/bad.js')).toEqual(['/a.js', '/b.js']);
    expect(toast.items.map((x) => x.text)).toEqual(['Admin extension failed to load: /bad.js']);
  });

  it('retries a failed import on the next call, with one toast per url', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {});
    let fail = true;
    const seen: string[] = [];
    ext.importer = async (u) => {
      seen.push(u);
      if (fail) throw new Error('boom');
    };
    await ext.boot({ elements: ['/a.js'] });
    await ext.ensureElements();
    await ext.ensureElements();
    expect(seen).toEqual(['/a.js', '/a.js']);
    expect(toast.items).toHaveLength(1);
    fail = false;
    await ext.ensureElements();
    await ext.ensureElements();
    expect(seen).toHaveLength(3);
    expect(toast.items).toHaveLength(1);
  });
});

describe('sanitising', () => {
  it('keeps only own string entries from strings, widgets and cells', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {});
    const hostile = JSON.parse('{"__proto__":{"polluted":"yes"},"constructor":"x","prototype":"y","ok":"fine","n":1,"o":{"a":1}}');
    ext.fetcher = (() => reply(hostile)) as unknown as typeof fetch;
    await ext.boot({ strings: ['/h.json'], widgets: hostile, cells: JSON.parse('{"__proto__":"x-evil","c":"x-c"}') });
    expect(t('ok')).toBe('fine');
    expect(t('n')).toBe('n');
    expect(t('constructor')).toBe('constructor');
    expect(t('polluted')).toBe('polluted');
    expect(({} as Record<string, unknown>).polluted).toBeUndefined();
    expect(Object.keys(ext.m.widgets)).toEqual(['ok']);
    expect(ext.widget('constructor')).toBeUndefined();
    expect(Object.keys(ext.m.cells)).toEqual(['c']);
  });

  it('drops malformed list entries', () => {
    const m = parseExtensions({
      panels: [null, 'x', { tag: 3 }, { tag: 'x-p', title: 'P', types: ['a', 1], policy: 4 }],
      views: [{ tag: 'x-v' }, { tag: 'x-v', path: '/v' }],
      slots: [{ tag: 'x-s' }, { tag: 'x-s', at: 'editor.before' }],
      elements: ['/a.js', 5],
    });
    expect(m.panels).toEqual([{ tag: 'x-p', title: 'P', types: ['a'], policy: undefined }]);
    expect(m.views.map((v) => v.path)).toEqual(['/v']);
    expect(m.slots).toHaveLength(1);
    expect(m.elements).toEqual(['/a.js']);
  });
});

describe('policies', () => {
  it('panels, actions, slots and dashboard widgets honour policy', async () => {
    await ext.boot({
      panels: [{ tag: 'x-a', policy: 'p.a' }, { tag: 'x-b' }],
      actions: [{ tag: 'x-c', policy: 'p.a' }],
      dashboard: [{ tag: 'x-d', policy: 'p.a' }, { tag: 'x-e' }],
      slots: [{ tag: 'x-f', at: 'editor.before', policy: 'p.a' }, { tag: 'x-g', at: 'editor.before' }],
    });
    session.grants = [];
    expect(ext.panels('t').map((x) => x.tag)).toEqual(['x-b']);
    expect(ext.actions('t')).toEqual([]);
    expect(ext.dashboard().map((x) => x.tag)).toEqual(['x-e']);
    expect(ext.slots('editor.before').map((x) => x.tag)).toEqual(['x-g']);
    session.grants = [{ policy: 'p.a', role: 'r' }] as never;
    expect(ext.panels('t')).toHaveLength(2);
    expect(ext.actions('t')).toHaveLength(1);
    expect(ext.dashboard()).toHaveLength(2);
    expect(ext.slots('editor.before')).toHaveLength(2);
  });
});

describe('navigation', () => {
  it('normalises admin-relative paths and refuses anything else', () => {
    expect(safePath('items/1')).toBe('/items/1');
    expect(safePath('/items/1?x=1')).toBe('/items/1?x=1');
    for (const bad of ['https://evil.test', '//evil.test', '/\\evil.test', 'javascript:alert(1)', '', 5]) expect(safePath(bad)).toBeNull();
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    navigateTo('https://evil.test');
    expect(warn).toHaveBeenCalled();
  });
});

describe('mountElement', () => {
  it('sets properties (not attributes), ctx first, and forwards events', () => {
    const order: string[] = [];
    class El extends HTMLElement {
      set ctx(v: unknown) {
        order.push('ctx');
      }
      set value(v: unknown) {
        order.push('value');
      }
      connectedCallback() {
        order.push('connected');
      }
    }
    customElements.define('x-mount-test', El);
    const box = document.body.appendChild(document.createElement('div'));
    const got: unknown[] = [];
    const m = mountElement(box, 'x-mount-test', { value: 'a' }, { ctx: getCtx(), on: { change: (e) => got.push(e.detail) } });
    expect(order).toEqual(['ctx', 'value', 'connected']);
    expect(m.el.hasAttribute('value')).toBe(false);
    m.el.dispatchEvent(new CustomEvent('change', { detail: { value: 1 }, bubbles: true }));
    expect(got).toEqual([{ value: 1 }]);
    m.update({ value: 'a' });
    expect(order.filter((x) => x === 'value')).toHaveLength(1);
    m.destroy();
    expect(box.children).toHaveLength(0);
  });

  it('ignores native events and details that are not objects', () => {
    customElements.define('x-guard-test', class extends HTMLElement {});
    const box = document.body.appendChild(document.createElement('div'));
    const got: string[] = [];
    const m = mountElement(box, 'x-guard-test', {}, { ctx: getCtx(), on: { change: () => got.push('change'), refresh: () => got.push('refresh'), navigate: () => got.push('navigate') } });
    m.el.dispatchEvent(new Event('change', { bubbles: true }));
    m.el.dispatchEvent(new Event('refresh'));
    m.el.dispatchEvent(new CustomEvent('change', { detail: 'str' }));
    m.el.dispatchEvent(new CustomEvent('navigate', { detail: null }));
    expect(got).toEqual([]);
    m.el.dispatchEvent(new CustomEvent('refresh'));
    m.el.dispatchEvent(new CustomEvent('navigate', { detail: { path: '/x' } }));
    expect(got).toEqual(['refresh', 'navigate']);
  });

  it('ctx follows the session', () => {
    session.principal = { kind: 'user', id: '1', name: 'A' } as never;
    session.grants = [{ policy: 'x.run', role: 'r' }] as never;
    const c = getCtx();
    expect(getCtx()).toBe(c);
    expect(c.principal.name).toBe('A');
    expect(c.can('x.run')).toBe(true);
    session.grants = [];
    expect(getCtx()).not.toBe(c);
    expect(getCtx().can('x.run')).toBe(false);
  });
});

describe('widget slot', () => {
  it('routes change events into the form and passes WidgetProps', async () => {
    let el!: HTMLElement & Record<string, unknown>;
    customElements.define(
      'x-widget-test',
      class extends HTMLElement {
        constructor() {
          super();
          el = this as never;
        }
      },
    );
    ext.importer = async () => {};
    await ext.boot({ elements: ['/e.js'], widgets: { color: 'x-widget-test' } });
    const field = { name: 'c', label: 'Colour', shape: { base: 'string' }, admin: { widget: 'color' } } as unknown as CompiledField;
    const form = new Form({ fields: { c: 'red' } as Record<string, unknown> });
    mounted.push(mount(FieldRowHost, { target: document.body, props: { form, field } }));
    await vi.waitFor(() => expect(el).toBeTruthy());
    await tick();
    expect(el.value).toBe('red');
    expect(el.field).toBe(field);
    expect(el.readonly).toBe(false);
    expect(el.locale).toBe('en');
    el.dispatchEvent(new CustomEvent('change', { detail: { value: 'blue' }, bubbles: true, composed: true }));
    flushSync();
    expect(form.values.fields.c).toBe('blue');
    expect(form.dirty).toBe(true);
  });

  it('a native change from an inner input of a light-DOM widget does not change the form', async () => {
    let input!: HTMLInputElement;
    customElements.define(
      'x-light-widget',
      class extends HTMLElement {
        connectedCallback() {
          input = this.appendChild(document.createElement('input'));
          input.value = 'typed';
        }
      },
    );
    ext.importer = async () => {};
    await ext.boot({ elements: ['/e.js'], widgets: { light: 'x-light-widget' } });
    const field = { name: 'c', label: 'Colour', shape: { base: 'string' }, admin: { widget: 'light' } } as unknown as CompiledField;
    const form = new Form({ fields: { c: 'red' } as Record<string, unknown> });
    mounted.push(mount(FieldRowHost, { target: document.body, props: { form, field } }));
    await vi.waitFor(() => expect(input).toBeTruthy());
    input.dispatchEvent(new Event('change', { bubbles: true }));
    flushSync();
    expect(form.values.fields.c).toBe('red');
    expect(form.dirty).toBe(false);
  });

  it('shows the error once: the widget gets it, the Field row does not render it', async () => {
    let el!: HTMLElement & Record<string, unknown>;
    customElements.define(
      'x-err-widget',
      class extends HTMLElement {
        constructor() {
          super();
          el = this as never;
        }
      },
    );
    ext.importer = async () => {};
    await ext.boot({ elements: ['/e.js'], widgets: { err: 'x-err-widget' } });
    const field = { name: 'c', label: 'Colour', shape: { base: 'string' }, admin: { widget: 'err' } } as unknown as CompiledField;
    const form = new Form({ fields: { c: 'red' } as Record<string, unknown> });
    form.setErrors([{ path: 'c', code: 'x', message: 'Bad colour' }]);
    mounted.push(mount(FieldRowHost, { target: document.body, props: { form, field } }));
    await vi.waitFor(() => expect(el).toBeTruthy());
    await tick();
    expect(el.error).toBe('Bad colour');
    expect(document.body.querySelector('.h-field__error')).toBeNull();
  });

  it('an unknown widget name falls back with one warning', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    expect(ext.widget('nope')).toBeUndefined();
    expect(ext.widget('nope')).toBeUndefined();
    expect(warn).toHaveBeenCalledTimes(1);
  });
});

describe('views', () => {
  const views = [
    { path: '/tools/import', tag: 'x-view-test', menu: 'Tools', title: 'Import', policy: 'import.run' },
    { path: '/tools/open', tag: 'x-view-test', menu: 'Tools', title: 'Open' },
  ];

  it('menu entries are grouped and gated by can()', async () => {
    await ext.boot({ views });
    session.grants = [];
    expect(ext.menu().map((g) => g.views.map((v) => v.path))).toEqual([['/tools/open']]);
    session.grants = [{ policy: 'import.run', role: 'r' }] as never;
    expect(ext.menu()).toHaveLength(1);
    expect(ext.menu()[0]!.views.map((v) => v.path)).toEqual(['/tools/import', '/tools/open']);
  });

  it('a registered route renders the element with ViewProps; without the policy it is refused', async () => {
    setConfig({ prefix: '' });
    let el!: HTMLElement & Record<string, unknown>;
    customElements.define(
      'x-view-test',
      class extends HTMLElement {
        constructor() {
          super();
          el = this as never;
        }
      },
    );
    ext.importer = async () => {};
    await ext.boot({ elements: ['/e.js'], views });
    session.principal = { kind: 'user', name: 'A' } as never;
    session.grants = [];
    router.define(ext.m.views.map((v) => ({ path: v.path, load: async () => ({ default: ViewHost }) })));
    await router.navigate('/tools/import?x=1', { force: true });
    mounted.push(mount(ViewHost, { target: document.body, props: { params: {} } }));
    await tick();
    expect(document.body.querySelector('[data-extension]')).toBeNull();
    session.grants = [{ policy: 'import.run', role: 'r' }] as never;
    await vi.waitFor(() => expect(el).toBeTruthy());
    expect(el.query).toEqual({ x: '1' });
    expect(el.params).toEqual({});
    expect(document.body.querySelector('h1')?.textContent).toBe('Import');
  });
});
