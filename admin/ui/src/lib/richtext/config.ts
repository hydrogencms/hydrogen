// Kept free of ProseMirror imports: the field widget reads it from the initial chunk.

export interface RichConfig {
  /** Allowed mark names; undefined allows all. */
  marks?: string[];
  /** Allowed node names; undefined allows all. */
  nodes?: string[];
}

/** Reads the allowlists from a compiled field's `options`. */
export function configOf(options?: Record<string, unknown>): RichConfig {
  const list = (v: unknown) => (Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : undefined);
  return { marks: list(options?.marks), nodes: list(options?.nodes) };
}
