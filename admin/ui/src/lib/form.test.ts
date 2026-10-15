import { describe, expect, it } from 'vitest';
import { ApiError } from './api/client';
import { Form } from './form.svelte';

describe('Form', () => {
  it('tracks dirty state and resets', () => {
    const f = new Form({ title: 'a', tags: ['x'] });
    expect(f.dirty).toBe(false);
    f.values.title = 'b';
    expect(f.dirty).toBe(true);
    f.values.title = 'a';
    expect(f.dirty).toBe(false);
    f.values.tags.push('y');
    expect(f.dirty).toBe(true);
    f.reset({ title: 'c', tags: [] });
    expect(f.dirty).toBe(false);
  });

  it('maps API field errors by path and strips the prefix', async () => {
    const f = new Form({ title: '' }, { prefix: 'fields.' });
    const ok = await f.submit(async () => {
      throw new ApiError(422, 'invalid', 'invalid', { fields: [{ path: 'fields.title', code: 'required', message: 'Required' }, { path: 'fields.body[2].text', code: 'x', message: 'Bad' }] });
    });
    expect(ok).toBe(false);
    expect(f.error('title')).toBe('Required');
    expect(f.errorsUnder('body')).toEqual({ 'body[2].text': 'Bad' });
    f.clearError('title');
    expect(f.error('title')).toBeUndefined();
  });

  it('is pending during submit, blocks a second submit, and marks saved on success', async () => {
    const f = new Form({ n: 1 });
    f.values.n = 2;
    let release!: () => void;
    const p = f.submit(() => new Promise<void>((r) => (release = r)));
    expect(f.pending).toBe(true);
    expect(await f.submit(async () => {})).toBe(false);
    release();
    expect(await p).toBe(true);
    expect(f.pending).toBe(false);
    expect(f.dirty).toBe(false);
  });

  it('rethrows server errors', async () => {
    const f = new Form({});
    await expect(f.submit(async () => { throw new ApiError(500, 'internal', 'x'); })).rejects.toBeInstanceOf(ApiError);
    expect(f.pending).toBe(false);
  });
});
