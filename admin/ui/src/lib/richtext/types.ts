import type { RichConfig } from './schema';

/** The props of RichTextEditor: what a field widget or a block editor passes. */
export interface RichTextProps {
  /** The stored document (`{type: 'doc', content: [...]}`); undefined or an empty document shows an empty editor. */
  value?: unknown;
  /** Allowed marks and nodes (a field's `options.marks` / `options.nodes`; see `configOf`). Undefined allows all. */
  config?: RichConfig;
  /** Locale being edited; the item search in the link popover shows names in it. */
  locale?: string;
  readonly?: boolean;
  /** An error message; the editor shows the invalid state (the surrounding Field prints the text). */
  error?: string;
  /** Accessible name of the editor (the field label). */
  label: string;
  /** `id` / `aria-describedby` from the surrounding Field's control props. */
  id?: string;
  'aria-describedby'?: string;
  placeholder?: string;
  /** Called with the stored document after each edit, or `undefined` when the editor is empty. */
  onchange?: (value: unknown) => void;
  /** Rows of minimum height (default 8). */
  rows?: number;
  /** Show "Edit as JSON" in the toolbar's menu (default true). */
  json?: boolean;
}
