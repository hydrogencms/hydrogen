import { getContext, setContext } from 'svelte';

/** What the generated form gives every widget below it. */
export interface FieldCtx {
  error(path: string): string | undefined;
  clearError(path: string): void;
  /** Every error of the form by path (the block editor routes `body[2].props.x` to the block). */
  errors?(): Record<string, string>;
  /** Whole form is read-only (no edit right, or the item is gone). */
  readonly: boolean;
  /** Locale being edited (reference search shows names in it). */
  locale: string;
  /** The type being edited (an upload type's file widget checks the file against it). */
  type?: string;
  /** A new item: slugs follow their source field. */
  isNew: boolean;
  namePattern?: string;
  /** The top-level field `name` waits for its shared content (a collaboration session is still seeding it). */
  loading?(name: string): boolean;
  /** A collaboration session is bound: values may change under the editor from other people's edits. */
  collab?(): boolean;
  /** Set when the locale being edited inherits from another. */
  readonly overlay?: OverlayCtx;
}

/** Locale inheritance: the item is edited in a locale that inherits its translatable fields from `parent`. */
export interface OverlayCtx {
  parent: string;
  state(name: string): 'inherited' | 'overridden';
  /** The locale an inherited field's value comes from. */
  source(name: string): string;
  /** The inherited value (the parent's resolved one). */
  value(name: string): unknown;
  /** The form values plus the inherited fields (for conditions). */
  resolved(): Record<string, unknown>;
  override(name: string): void;
  reset(name: string): void;
}

const KEY = Symbol('h.fields');

export const provideFields = (c: FieldCtx) => setContext(KEY, c);
export const useFields = () => getContext<FieldCtx>(KEY);
