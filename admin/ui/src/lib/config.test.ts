import { describe, expect, it } from 'vitest';
import { configURL } from './config';

describe('configURL', () => {
  it('resolves next to the bundle in a build, at any prefix', () => {
    expect(configURL(false, './', 'http://h/admin/assets/index-x.js').pathname).toBe('/admin/config.json');
    expect(configURL(false, './', 'http://h/cms/admin/assets/index-x.js').pathname).toBe('/cms/admin/config.json');
  });

  it('uses the dev server base for unbundled modules', () => {
    expect(configURL(true, '/admin/', 'http://h/admin/src/lib/config.ts').pathname).toBe('/admin/config.json');
  });
});
