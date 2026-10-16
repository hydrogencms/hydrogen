import type { Participant } from './transport';

const CLS = 'h-collab-mark';
const CARD = 'h-collab-card';

const DESC = 'data-collab-desc';
const CONTROL = 'input:not([type=hidden]), textarea, select, [contenteditable=true], [role=textbox]';
let seq = 0;

const lum = (hex: string) => {
  const m = /^#([0-9a-f]{6})$/i.exec(hex);
  if (!m) return undefined;
  const v = [0, 2, 4].map((i) => parseInt(m[1]!.slice(i, i + 2), 16) / 255).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * v[0]! + 0.7152 * v[1]! + 0.0722 * v[2]!;
};
const ratio = (a: number, b: number) => (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
const toHex = (rgb: number[]) => '#' + rgb.map((c) => Math.round(c).toString(16).padStart(2, '0')).join('');

/**
 * A background and text colour for a name tag or avatar in the participant's colour that reach 4.5:1: white text,
 * on the colour darkened as far as needed (the palette's lighter colours do not carry white text as they are).
 */
export function readable(color: string): { bg: string; fg: string } {
  const m = /^#([0-9a-f]{6})$/i.exec(color);
  if (!m) return { bg: color, fg: '#fff' };
  const rgb = [0, 2, 4].map((i) => parseInt(m[1]!.slice(i, i + 2), 16));
  for (let k = 0; k <= 20; k++) {
    const bg = toHex(rgb.map((c) => c * (1 - k * 0.025)));
    const l = lum(bg)!;
    if (ratio(1, l) >= 4.5) return { bg, fg: '#fff' };
  }
  return { bg: '#000', fg: '#fff' };
}

function clear(root: HTMLElement) {
  for (const el of root.querySelectorAll<HTMLElement>(`.${CLS}, .${CARD}`)) {
    el.classList.remove(CLS, CARD);
    el.removeAttribute('data-collab-name');
    for (const p of ['--h-collab', '--h-collab-bg', '--h-collab-fg']) el.style.removeProperty(p);
  }
  for (const span of root.querySelectorAll<HTMLElement>(`[${DESC}]`)) {
    const id = span.id;
    for (const c of root.querySelectorAll<HTMLElement>(`[aria-describedby~="${id}"]`)) {
      const rest = (c.getAttribute('aria-describedby') ?? '').split(/\s+/).filter((x) => x && x !== id);
      if (rest.length) c.setAttribute('aria-describedby', rest.join(' '));
      else c.removeAttribute('aria-describedby');
    }
    span.remove();
  }
}

/** The tag a field carries for the participants focused in it. */
export function tagText(names: string[], tpl: (name: string) => string, many: (n: number) => string): string {
  return names.length === 1 ? tpl(names[0]!) : many(names.length);
}

/**
 * Outlines the fields (and the block cards around them) other participants have focused, with a name tag. Run
 * again after the DOM or the participants change; it only sets classes, attributes and a custom property.
 */
export function applyMarks(root: HTMLElement, others: Participant[], tpl: (name: string) => string, many: (n: number) => string): void {
  clear(root);
  const byPath = new Map<string, Participant[]>();
  for (const p of others) if (p.focus) byPath.set(p.focus, [...(byPath.get(p.focus) ?? []), p]);
  if (!byPath.size) return;
  const rows = new Map<string, HTMLElement>();
  for (const el of root.querySelectorAll<HTMLElement>('[data-field]')) rows.set(el.getAttribute('data-field')!, el);
  for (const [path, ps] of byPath) {
    const el = rows.get(path);
    if (!el) continue;
    const first = ps[0]!;
    el.classList.add(CLS);
    const c = readable(first.color);
    el.style.setProperty('--h-collab', first.color);
    el.style.setProperty('--h-collab-bg', c.bg);
    el.style.setProperty('--h-collab-fg', c.fg);
    const text = tagText(ps.map((p) => p.name), tpl, many);
    el.setAttribute('data-collab-name', text);
    // The tag is drawn by CSS, which assistive technology may skip: the field's control is described by real text.
    const control = el.querySelector<HTMLElement>(CONTROL);
    if (control) {
      const span = document.createElement('span');
      span.className = 'h-sr';
      span.id = `h-collab-desc-${++seq}`;
      span.setAttribute(DESC, '');
      span.textContent = text;
      el.append(span);
      control.setAttribute('aria-describedby', [control.getAttribute('aria-describedby'), span.id].filter(Boolean).join(' '));
    }
    const card = el.parentElement?.closest<HTMLElement>('[data-block-id]');
    if (card && !card.classList.contains(CLS)) {
      card.classList.add(CARD);
      card.style.setProperty('--h-collab', first.color);
    }
  }
}
