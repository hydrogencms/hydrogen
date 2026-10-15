import { afterEach } from 'vitest';
import { unmount } from 'svelte';

export const mounted: Array<Record<string, unknown>> = [];

afterEach(() => {
  for (const c of mounted.splice(0)) unmount(c);
  document.body.innerHTML = '';
});
