import type { Snippet } from 'svelte';

export interface Column<R> {
  key: string;
  label: string;
  /** Header is a button; fires onsort with the key. */
  sortable?: boolean;
  align?: 'start' | 'end';
  width?: string;
  /** Custom cell; default shows row[key]. */
  cell?: Snippet<[R]>;
}

export interface Sort {
  key: string;
  dir: 'asc' | 'desc';
}
