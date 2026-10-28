import { expect, it } from 'vitest';
import { itemStatusTone } from './status';

it('maps publication statuses to badge tones', () => {
  expect(itemStatusTone('published')).toBe('success');
  expect(itemStatusTone('changed')).toBe('warning');
  expect(itemStatusTone('draft')).toBe('neutral');
  expect(itemStatusTone()).toBe('neutral');
});
