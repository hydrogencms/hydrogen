import { describe, expect, it } from 'vitest';
import '../../lib/messages/collections';
import { ApiError } from '../../lib/api/client';
import { t } from '../../lib/i18n';
import { bulkResults, errText } from './bulk';

describe('bulk results', () => {
  it('words errors', () => {
    expect(errText(new ApiError(403, 'forbidden', 'nope'))).toBe(t('app.forbidden'));
    expect(errText(new ApiError(409, 'other', 'conflict'))).toBe('conflict');
    expect(errText(new Error('boom'))).toBe('boom');
    expect(errText('x')).toBe('x');
  });
  it('counts referrers, none when absent', () => {
    const withRefs = new ApiError(409, 'referenced', 'm', { body: { referrers: [1, 2] } });
    expect(errText(withRefs)).toBe(t('collections.bulk.referenced', { n: '2' }));
    expect(errText(new ApiError(409, 'referenced', 'm'))).toBe(t('collections.bulk.referenced', { n: '0' }));
  });
  it('pairs targets with outcomes and falls back to the id for a name', () => {
    const r = bulkResults([{ id: 'a', name: 'A' }, { id: 'b', name: '' }], [{ ok: true }, { ok: false, error: new Error('bad') }]);
    expect(r).toEqual([
      { id: 'a', name: 'A', ok: true, message: '' },
      { id: 'b', name: 'b', ok: false, message: 'bad' },
    ]);
  });
});
