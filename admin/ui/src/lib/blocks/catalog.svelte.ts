import { api } from '../api/client';
import { session } from '../auth/session.svelte';
import { schemaOf, SHARED, type BlockType, type Rule, type Schema } from './tree';

export interface TokenSet {
  name: string;
  values: string[];
}

/**
 * The block types and token sets of the project (`GET /blocks`), loaded once per session on first use. The
 * block editor and the property widgets read from it; nothing else imports it, so it stays in the editor chunk.
 */
class BlocksCatalog {
  blocks = $state.raw<BlockType[]>([]);
  tokens = $state.raw<TokenSet[]>([]);
  loaded = $state(false);
  error = $state<Error | null>(null);
  #inflight: Promise<void> | undefined;

  constructor() {
    session.onClear(() => this.reset());
  }

  load(): Promise<void> {
    if (this.loaded) return Promise.resolve();
    this.#inflight ??= (async () => {
      try {
        const r = (await api.get('/blocks' as never)) as unknown as { blocks?: BlockType[]; tokens?: TokenSet[] };
        this.blocks = (r.blocks ?? []).map((b) => ({ ...b, props: b.props ?? [], slots: b.slots ?? [] }));
        this.tokens = r.tokens ?? [];
        this.error = null;
        this.loaded = true;
      } catch (e) {
        this.error = e as Error;
        throw e;
      } finally {
        this.#inflight = undefined;
      }
    })();
    return this.#inflight;
  }

  reset() {
    this.blocks = [];
    this.tokens = [];
    this.loaded = false;
    this.error = null;
  }

  get(name: string): BlockType | undefined {
    return this.blocks.find((b) => b.name === name);
  }

  label(name: string): string {
    return this.get(name)?.label ?? name;
  }

  icon(name: string): string {
    return this.get(name)?.icon || (name === SHARED ? 'link' : 'square');
  }

  token(set: string): string[] {
    return this.tokens.find((t) => t.name === set)?.values ?? [];
  }

  /** The types a list with this rule accepts, in the rule's order (unknown names are skipped). */
  allowed(rule: Rule): BlockType[] {
    return rule.allow.map((n) => this.get(n)).filter((b): b is BlockType => !!b);
  }

  /** Types that can be the target of a shared block (`blockset` types). */
  get sharedTo(): string[] {
    const to = this.get(SHARED)?.props.find((f) => f.name === 'item')?.shape.to;
    return Array.isArray(to) ? (to as string[]) : [];
  }

  schema(root: Rule): Schema {
    return schemaOf(this.blocks, root);
  }
}

export const blocksCatalog = new BlocksCatalog();
