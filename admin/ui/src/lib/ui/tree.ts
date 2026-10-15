export interface TreeNode {
  id: string;
  label: string;
  icon?: string;
  /** True when the node may have children that are not loaded yet. */
  hasChildren?: boolean;
  children?: TreeNode[];
  disabled?: boolean;
  data?: unknown;
}

export interface FlatNode {
  node: TreeNode;
  level: number;
  parent: string | undefined;
  size: number;
  pos: number;
  expandable: boolean;
  open: boolean;
}

export type DropPosition = 'before' | 'after' | 'inside';

/** The visible rows of a tree in order. `childrenOf` returns loaded children (or undefined when unloaded). */
export function flatten(
  roots: TreeNode[],
  expanded: Set<string>,
  childrenOf: (n: TreeNode) => TreeNode[] | undefined,
): FlatNode[] {
  const out: FlatNode[] = [];
  const walk = (list: TreeNode[], level: number, parent: string | undefined) => {
    list.forEach((node, i) => {
      const kids = childrenOf(node);
      const expandable = kids ? kids.length > 0 : !!node.hasChildren;
      const open = expandable && expanded.has(node.id);
      out.push({ node, level, parent, size: list.length, pos: i + 1, expandable, open });
      if (open && kids) walk(kids, level + 1, node.id);
    });
  };
  walk(roots, 1, undefined);
  return out;
}

export function isDescendant(flat: FlatNode[], id: string, ancestor: string): boolean {
  const parent = new Map(flat.map((f) => [f.node.id, f.parent]));
  for (let p = parent.get(id); p !== undefined; p = parent.get(p)) if (p === ancestor) return true;
  return false;
}

export function dropPosition(y: number, rect: { top: number; height: number }): DropPosition {
  const f = (y - rect.top) / rect.height;
  return f < 0.25 ? 'before' : f > 0.75 ? 'after' : 'inside';
}
