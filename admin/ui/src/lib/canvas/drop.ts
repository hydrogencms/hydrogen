// Where a dragged block would land, from the frame's layout. Pure.
import { listPath } from './paths';

export interface LayoutNode {
  path: string;
  rect: [number, number, number, number];
  block?: string;
  text?: boolean;
}
export interface CanvasDrop {
  /** Frame path of the target list ('fields/body', 'fields/body/2/slots/cards'). */
  list: string;
  /** Index in the list before the move (tree.moveTo semantics). */
  index: number;
  /** The indicator bar, in frame coordinates. */
  line: [number, number, number, number];
}

type Rect = LayoutNode['rect'];
const BAR = 2;

const inside = (r: Rect, x: number, y: number) => x >= r[0] && x <= r[0] + r[2] && y >= r[1] && y <= r[1] + r[3];
const dist = (r: Rect, x: number, y: number) => Math.hypot(Math.max(r[0] - x, 0, x - (r[0] + r[2])), Math.max(r[1] - y, 0, y - (r[1] + r[3])));

/** The block path that owns a list path ('fields/body/2/slots/cards' -> 'fields/body/2'), or undefined for a field. */
function ownerOf(list: string): string | undefined {
  const at = list.lastIndexOf('/slots/');
  return at < 0 ? undefined : list.slice(0, at);
}

export function dropAt(nodes: LayoutNode[], dragged: string, x: number, y: number, allowed: (list: string) => boolean): CanvasDrop | null {
  const blocks = nodes.filter((n) => n.block !== undefined && n.path !== dragged && !n.path.startsWith(dragged + '/') && listPath(n.path));
  const byPath = new Map(blocks.map((n) => [n.path, n]));
  const own = listPath(dragged);
  const siblings = (list: string) => nodes.filter((n) => n.block !== undefined && listPath(n.path)?.list === list);

  const side = (n: LayoutNode, list: string): { horizontal: boolean; before: boolean } => {
    const [nx, ny, nw, nh] = n.rect;
    const horizontal = siblings(list).some((s) => {
      if (s.path === n.path) return false;
      const [sx, sy, sw, sh] = s.rect;
      const overlap = Math.min(ny + nh, sy + sh) - Math.max(ny, sy);
      if (overlap <= 0.5 * Math.min(nh, sh)) return false;
      return sx + sw <= nx + nw / 2 || sx >= nx + nw / 2;
    });
    return horizontal ? { horizontal, before: x < nx + nw / 2 } : { horizontal, before: y < ny + nh / 2 };
  };

  const tryNode = (start: LayoutNode): CanvasDrop | null | undefined => {
    let n: LayoutNode | undefined = start;
    while (n) {
      const lp = listPath(n.path)!;
      if (allowed(lp.list)) {
        const s = side(n, lp.list);
        const index = s.before ? lp.index : lp.index + 1;
        if (own && own.list === lp.list && (index === own.index || index === own.index + 1)) return null;
        const [rx, ry, rw, rh] = n.rect;
        let line: CanvasDrop['line'];
        if (s.horizontal) line = [(s.before ? rx : rx + rw) - BAR / 2, ry, BAR, rh];
        else line = [rx, (s.before ? ry : ry + rh) - BAR / 2, rw, BAR];
        return { list: lp.list, index, line };
      }
      const owner = ownerOf(lp.list);
      n = owner ? byPath.get(owner) : undefined;
    }
    return undefined;
  };

  const hit = blocks.filter((n) => inside(n.rect, x, y)).sort((a, b) => b.path.length - a.path.length);
  for (const n of hit) {
    const r = tryNode(n);
    if (r !== undefined) return r;
  }
  if (hit.length) return null;
  const pool = own ? blocks.filter((n) => listPath(n.path)?.list === own.list) : [];
  const near = (pool.length ? pool : blocks).slice().sort((a, b) => dist(a.rect, x, y) - dist(b.rect, x, y));
  for (const n of near) {
    const r = tryNode(n);
    if (r !== undefined) return r;
  }
  return null;
}
