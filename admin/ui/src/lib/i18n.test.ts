import { expect, it } from 'vitest';
import { register, t } from './i18n';

it('translates, interpolates, and falls back to the key', () => {
  expect(t('common.save')).toBe('Save');
  expect(t('login.throttled', { seconds: 5 })).toBe('Too many attempts. Try again in 5 s.');
  expect(t('no.such.key')).toBe('no.such.key');
  register({ 'x.y': 'Hello {name} {missing}' });
  expect(t('x.y', { name: 'A' })).toBe('Hello A {missing}');
});
