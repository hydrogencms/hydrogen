import { describe, expect, it } from 'vitest';
import { ApiError } from './api/client';
import { errorMessage, generatePassword } from './errors';

describe('errors', () => {
  it('formats a 403 with the policy and others with the message', () => {
    expect(errorMessage(new ApiError(403, 'forbidden', 'content.edit'))).toBe("You can't do this: Required: content.edit");
    expect(errorMessage(new ApiError(422, 'invalid', 'bad'))).toBe('bad');
  });
  it('generates passwords of the requested length from the alphabet', () => {
    const p = generatePassword(64);
    expect(p).toHaveLength(64);
    expect(p).toMatch(/^[a-zA-Z2-9_-]+$/);
    expect(generatePassword(20)).not.toBe(generatePassword(20));
  });
});
