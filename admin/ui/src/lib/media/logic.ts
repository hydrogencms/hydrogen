// Pure logic of the media library: folders, upload rules, previews and the focal point. Free of Svelte.
import { config } from '../config';

export interface Focal {
  x: number;
  y: number;
}

/** The `file` field value. The server owns every key but `name` and `focal`. */
export interface FileValue {
  id: string;
  name: string;
  mime: string;
  size: number;
  width?: number;
  height?: number;
  focal?: Focal;
}

export interface UploadSpec {
  accept?: string[];
  max_size?: number | string;
}

// ---- folders

/** `''`, `marketing//2026/` -> `/`, `/marketing/2026`. */
export function normFolder(s: string | undefined | null): string {
  const parts = (s ?? '').split('/').map((p) => p.trim()).filter(Boolean);
  return '/' + parts.join('/');
}

export function ancestorsOf(path: string): string[] {
  const parts = normFolder(path).split('/').filter(Boolean);
  return parts.map((_, i) => '/' + parts.slice(0, i + 1).join('/'));
}

export interface FolderNode {
  id: string;
  label: string;
  children: FolderNode[];
}

/** The folder tree of the distinct paths in use: every ancestor is a node; siblings sort by name. */
export function folderTree(paths: Iterable<string | undefined | null>): FolderNode[] {
  const roots: FolderNode[] = [];
  const byId = new Map<string, FolderNode>();
  for (const p of paths) {
    for (const id of ancestorsOf(p ?? '')) {
      if (byId.has(id)) continue;
      const node: FolderNode = { id, label: id.slice(id.lastIndexOf('/') + 1), children: [] };
      byId.set(id, node);
      const parent = byId.get(id.slice(0, id.lastIndexOf('/')));
      (parent ? parent.children : roots).push(node);
    }
  }
  const sort = (l: FolderNode[]) => {
    l.sort((a, b) => a.label.localeCompare(b.label));
    for (const n of l) sort(n.children);
  };
  sort(roots);
  return roots;
}

// ---- upload rules

/** Whether `mime` (or the file name's extension, for `.png` patterns) satisfies the `accept` patterns; no patterns accept all. */
export function matchAccept(accept: string[] | undefined, mime: string, name = ''): boolean {
  if (!accept || accept.length === 0) return true;
  const m = mime.toLowerCase();
  const n = name.toLowerCase();
  return accept.some((raw) => {
    const p = raw.trim().toLowerCase();
    if (!p || p === '*' || p === '*/*') return true;
    if (p.startsWith('.')) return n.endsWith(p);
    if (p.endsWith('/*')) return m.startsWith(p.slice(0, -1));
    return m === p;
  });
}

const UNITS: Record<string, number> = { b: 1, kb: 1024, mb: 1024 ** 2, gb: 1024 ** 3 };

/** `20MB`, `512 kb`, `1048576` -> bytes; undefined when not a size. */
export function parseSize(v: number | string | undefined | null): number | undefined {
  if (typeof v === 'number') return Number.isFinite(v) && v > 0 ? v : undefined;
  const m = /^\s*(\d+(?:\.\d+)?)\s*([kmg]?b?)?\s*$/i.exec(v ?? '');
  if (!m) return undefined;
  const unit = (m[2] ?? '').toLowerCase();
  return Math.round(Number(m[1]) * (UNITS[unit.endsWith('b') ? unit : unit ? unit + 'b' : 'b'] ?? 1));
}

export function formatSize(bytes: number | undefined): string {
  if (bytes === undefined || !Number.isFinite(bytes)) return '';
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB'];
  let v = bytes / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || Number.isInteger(v) ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

/** The configured server-wide upload limit in bytes (admin config), when known. */
export const maxUpload = (): number | undefined => parseSize(config().media?.maxUpload);

/**
 * Why a file does not fit the type's upload rules: `type` (not accepted), `size` (too big) or null. `limit` is the
 * server-wide upload limit; the smaller of it and the type's `max_size` applies.
 */
export function checkFile(spec: UploadSpec | undefined, file: { type: string; name: string; size: number }, limit?: number): 'type' | 'size' | null {
  if (!matchAccept(spec?.accept, file.type || mimeByName(file.name), file.name)) return 'type';
  const caps = [parseSize(spec?.max_size), limit].filter((n): n is number => n !== undefined);
  return caps.length && file.size > Math.min(...caps) ? 'size' : null;
}

const EXT: Record<string, string> = {
  png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif', webp: 'image/webp', svg: 'image/svg+xml',
  avif: 'image/avif', pdf: 'application/pdf', mp4: 'video/mp4', webm: 'video/webm', mp3: 'audio/mpeg', wav: 'audio/wav',
};

/** A guess for files the browser gave no type to (the server sniffs the real one). */
export function mimeByName(name: string): string {
  return EXT[name.slice(name.lastIndexOf('.') + 1).toLowerCase()] ?? '';
}

/** The first type (the preferred one first) that accepts the file. */
export function pickType(specs: { name: string; upload?: UploadSpec }[], file: { type: string; name: string; size: number }, preferred?: string, limit?: number): string | undefined {
  const ordered = [...specs].sort((a, b) => Number(b.name === preferred) - Number(a.name === preferred));
  return ordered.find((s) => checkFile(s.upload, file, limit) === null)?.name;
}

/** Why no type takes the file: `size` when some type accepts its kind but not its size, else `type`. */
export function whyRejected(specs: { name: string; upload?: UploadSpec }[], file: { type: string; name: string; size: number }, limit?: number): 'type' | 'size' {
  return specs.some((s) => checkFile(s.upload, file, limit) === 'size') ? 'size' : 'type';
}

/** The files of a drop: plain files only (a dropped folder has no bytes to send). Read it synchronously in the handler. */
export function droppedFiles(dt: Pick<DataTransfer, 'items' | 'files'>): File[] {
  const out: File[] = [];
  if (dt.items?.length) {
    for (const item of Array.from(dt.items)) {
      if (item.kind !== 'file') continue;
      const entry = (item as DataTransferItem & { webkitGetAsEntry?: () => { isDirectory: boolean } | null }).webkitGetAsEntry?.();
      if (entry?.isDirectory) continue;
      const f = item.getAsFile();
      if (f) out.push(f);
    }
    return out;
  }
  return Array.from(dt.files ?? []);
}

// ---- previews

export type PreviewKind = 'image' | 'video' | 'audio' | 'pdf' | 'file';

export function previewKind(mime: string | undefined): PreviewKind {
  const m = (mime ?? '').toLowerCase();
  if (m.startsWith('image/')) return 'image';
  if (m.startsWith('video/')) return 'video';
  if (m.startsWith('audio/')) return 'audio';
  if (m === 'application/pdf') return 'pdf';
  return 'file';
}

/** Where a stored file is served (public, content addressed): under the configured asset base, `/assets` by default. */
export function assetPath(f: { id: string; name: string }, base = config().media?.assetBase): string {
  return `${(base || '/assets').replace(/\/+$/, '')}/${f.id}/${encodeURIComponent(f.name)}`;
}

/** Reads the `file` field of an item's fields; undefined when it is not a file value. */
export function fileOf(v: unknown): FileValue | undefined {
  if (!v || typeof v !== 'object') return undefined;
  const f = v as Partial<FileValue>;
  return typeof f.id === 'string' && f.id ? (f as FileValue) : undefined;
}

export function dimensions(f: Pick<FileValue, 'width' | 'height'>): string {
  return f.width && f.height ? `${f.width} × ${f.height}` : '';
}

// ---- focal point

export const clamp01 = (n: number) => Math.min(1, Math.max(0, n));
const r3 = (n: number) => Math.round(n * 1000) / 1000;

/** The focal point under a pointer over the image's box. */
export function focalFromPoint(rect: { left: number; top: number; width: number; height: number }, clientX: number, clientY: number): Focal {
  if (rect.width <= 0 || rect.height <= 0) return { x: 0.5, y: 0.5 };
  return { x: r3(clamp01((clientX - rect.left) / rect.width)), y: r3(clamp01((clientY - rect.top) / rect.height)) };
}

export const FOCAL_STEP = 0.05;
export const FOCAL_FINE = 0.01;

/** The point after an arrow key (5 %, 1 % with Shift) from the current one (the centre when unset); undefined for other keys. */
export function focalStep(f: Focal | undefined, key: string, fine = false): Focal | undefined {
  const d = fine ? FOCAL_FINE : FOCAL_STEP;
  const at = f ?? { x: 0.5, y: 0.5 };
  switch (key) {
    case 'ArrowLeft':
      return { x: r3(clamp01(at.x - d)), y: at.y };
    case 'ArrowRight':
      return { x: r3(clamp01(at.x + d)), y: at.y };
    case 'ArrowUp':
      return { x: at.x, y: r3(clamp01(at.y - d)) };
    case 'ArrowDown':
      return { x: at.x, y: r3(clamp01(at.y + d)) };
  }
  return undefined;
}

export const focalPercent = (f: Focal | undefined) => ({ x: Math.round((f?.x ?? 0.5) * 100), y: Math.round((f?.y ?? 0.5) * 100) });

/** CSS `object-position` that keeps the focal point in view when an image is cropped. */
export function objectPosition(f: Focal | undefined): string {
  const p = focalPercent(f);
  return `${p.x}% ${p.y}%`;
}

export function validFocal(f: unknown): Focal | undefined {
  const v = f as Partial<Focal> | undefined;
  return v && typeof v.x === 'number' && typeof v.y === 'number' ? { x: clamp01(v.x), y: clamp01(v.y) } : undefined;
}

// ---- tags

/** Adds the comma separated names in `raw` to `tags`: trimmed, empty and duplicate ones dropped. */
export function addTags(tags: string[], raw: string): string[] {
  const out = [...tags];
  for (const p of raw.split(',')) {
    const t = p.trim();
    if (t && !out.includes(t)) out.push(t);
  }
  return out;
}
