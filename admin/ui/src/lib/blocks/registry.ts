// Lets the canvas find the BlocksModel of a top-level blocks field. Reactive: readers re-run when fields come and go.
import { SvelteMap } from 'svelte/reactivity';
import type { BlocksModel } from './model.svelte';

const models = new SvelteMap<string, BlocksModel>();

/** Registers `m` under the field's form path; returns the function that removes it (only if still its entry). */
export function registerBlocksModel(path: string, m: BlocksModel): () => void {
  models.set(path, m);
  return () => {
    if (models.get(path) === m) models.delete(path);
  };
}

export function blocksModel(path: string): BlocksModel | undefined {
  return models.get(path);
}
