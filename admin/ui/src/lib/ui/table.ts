import type { Snippet } from 'svelte';

export interface Column<R> {
  key: string;
  label: string;
  /** Header is a button; fires onsort with the key. */
  sortable?: boolean;
  align?: 'start' | 'end';
  width?: string;
  /** Hidden below 768 px (rows become stacked cards; keep name, status and updated). */
  mobile?: 'hide';
  /** Tabular figures, right-aligned. */
  num?: boolean;
  /** Custom cell; default shows row[key]. */
  cell?: Snippet<[R, Column<R>]>;
}

export interface Sort {
  key: string;
  dir: 'asc' | 'desc';
}
