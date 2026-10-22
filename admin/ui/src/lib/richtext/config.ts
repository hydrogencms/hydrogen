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

/** One attribute of a declared rich text node or mark (`GET /richtext`). */
export interface RichAttrDef {
  name: string;
  type: 'string' | 'enum' | 'int' | 'bool' | 'url';
  values?: string[];
  min?: number;
  max?: number;
  maxLength?: number;
  required: boolean;
  default?: unknown;
}

export interface RichNodeDef {
  name: string;
  label: string;
  tag: string;
  inline: boolean;
  content: 'blocks' | 'inline' | 'none' | 'parts';
  parts: string[];
  /** Named by a parent: valid only inside it. */
  part: boolean;
  attrs: RichAttrDef[];
}

export interface RichMarkDef {
  name: string;
  label: string;
  tag: string;
  attrs: RichAttrDef[];
}

/** The project's custom rich text vocabulary. */
export interface RichCustom {
  nodes: RichNodeDef[];
  marks: RichMarkDef[];
}
