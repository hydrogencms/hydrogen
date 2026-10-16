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
  /** A new item: slugs follow their source field. */
  isNew: boolean;
  namePattern?: string;
  /** The top-level field `name` waits for its shared content (a collaboration session is still seeding it). */
  loading?(name: string): boolean;
  /** A collaboration session is bound: values may change under the editor from other people's edits. */
  collab?(): boolean;
}

const KEY = Symbol('h.fields');

export const provideFields = (c: FieldCtx) => setContext(KEY, c);
export const useFields = () => getContext<FieldCtx>(KEY);
