import type { Binding } from './bind.svelte';

/** What the editor gives rich text editors (and anything else bound to the session) through Svelte context. */
export interface CollabCtx {
  /** The live binding, or undefined while there is none (no session, still joining). */
  readonly binding: Binding | undefined;
}

export const COLLAB_CTX = Symbol('h.collab');
