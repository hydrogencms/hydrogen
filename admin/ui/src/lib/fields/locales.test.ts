import { afterEach, describe, expect, it } from 'vitest';
import { channels } from '../channels.svelte';
import { setConfig } from '../config';
import { knownLocales } from './locales';

const ch = (name: string, languages: string[]) => ({ name, languages }) as (typeof channels.items)[number];

describe('knownLocales', () => {
  afterEach(() => {
    channels.items = [];
    setConfig({ defaultLocale: undefined });
  });

  it('puts the default locale first when every channel is shown', () => {
    channels.items = [ch('cn', ['cn']), ch('de', ['de']), ch('en', ['en'])];
    expect(knownLocales()).toEqual(['cn', 'de', 'en']);
    setConfig({ defaultLocale: 'en' });
    expect(knownLocales()).toEqual(['en', 'cn', 'de']);
    setConfig({ defaultLocale: 'xx' });
    expect(knownLocales()).toEqual(['cn', 'de', 'en']);
  });
});
