const SEL =
  'a[href],button:not([disabled]),input:not([disabled]):not([type=hidden]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';

export function focusables(root: HTMLElement): HTMLElement[] {
  return [...root.querySelectorAll<HTMLElement>(SEL)].filter((e) => !e.hasAttribute('hidden') && e.getAttribute('aria-hidden') !== 'true');
}

/** Keep Tab / Shift+Tab inside `root`. Returns true when it handled the event. */
export function trapTab(e: KeyboardEvent, root: HTMLElement): boolean {
  if (e.key !== 'Tab') return false;
  const f = focusables(root);
  if (f.length === 0) {
    e.preventDefault();
    root.focus();
    return true;
  }
  const first = f[0];
  const last = f[f.length - 1];
  const active = document.activeElement;
  if (e.shiftKey && (active === first || active === root)) {
    e.preventDefault();
    last.focus();
    return true;
  }
  if (!e.shiftKey && active === last) {
    e.preventDefault();
    first.focus();
    return true;
  }
  return false;
}

const origins = new WeakMap<Node, Node>();

/** Svelte action: move the node to document.body (dialogs, menus escaping overflow). Remembers where it came from. */
export function portal(node: HTMLElement) {
  if (node.parentNode) origins.set(node, node.parentNode);
  document.body.appendChild(node);
  return { destroy: () => node.remove() };
}

/** True when `n` is inside `root`, or inside a portaled layer that was opened from within `root`. */
export function within(root: Node | undefined | null, n: Node): boolean {
  for (let cur: Node | null = n, hops = 0; cur && hops < 50; hops++) {
    if (root?.contains(cur)) return true;
    let layer: Node | null = cur;
    while (layer && !origins.has(layer)) layer = layer.parentNode;
    cur = layer ? (origins.get(layer) ?? null) : null;
  }
  return false;
}
