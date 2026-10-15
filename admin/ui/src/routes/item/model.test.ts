import { describe, expect, it, vi } from 'vitest';
import { confirmDiscard } from './model';

describe('confirmDiscard', () => {
  it('does not ask when nothing is unsaved', () => {
    const ask = vi.fn(() => false);
    expect(confirmDiscard(false, ask)).toBe(true);
    expect(ask).not.toHaveBeenCalled();
  });
  it('asks when dirty and follows the answer', () => {
    const ask = vi.fn(() => false);
    expect(confirmDiscard(true, ask)).toBe(false);
    expect(ask).toHaveBeenCalledWith('Reload and discard your changes?');
    expect(confirmDiscard(true, () => true)).toBe(true);
  });
});
