export type StatusTone = 'success' | 'warning' | 'neutral';

/** Badge tone of an item's publication status. */
export function itemStatusTone(status?: string): StatusTone {
  if (status === 'published') return 'success';
  if (status === 'changed') return 'warning';
  return 'neutral';
}
