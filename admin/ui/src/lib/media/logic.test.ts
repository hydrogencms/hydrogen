import { describe, expect, it } from 'vitest';
import {
  addTags, ancestorsOf, checkFile, clamp01, focalFromPoint, focalStep, folderTree, formatSize, matchAccept, normFolder,
  objectPosition, parseSize, pickType, previewKind, assetPath, fileOf, dimensions, droppedFiles, whyRejected,
} from './logic';

describe('folders', () => {
  it('normalises paths', () => {
    expect(normFolder('')).toBe('/');
    expect(normFolder(undefined)).toBe('/');
    expect(normFolder('marketing//2026/')).toBe('/marketing/2026');
    expect(normFolder(' /a / b ')).toBe('/a/b');
  });
  it('lists ancestors', () => {
    expect(ancestorsOf('/a/b/c')).toEqual(['/a', '/a/b', '/a/b/c']);
    expect(ancestorsOf('/')).toEqual([]);
  });
  it('builds the tree from distinct paths, adding missing parents', () => {
    const t = folderTree(['/marketing/2026', '/legal', '/', undefined, '/marketing/2025', '/marketing/2026']);
    expect(t.map((n) => n.id)).toEqual(['/legal', '/marketing']);
    expect(t[1]!.children.map((n) => [n.id, n.label])).toEqual([
      ['/marketing/2025', '2025'],
      ['/marketing/2026', '2026'],
    ]);
  });
  it('is empty without folders', () => expect(folderTree(['/', ''])).toEqual([]));
});

describe('accept', () => {
  it('matches mime patterns', () => {
    expect(matchAccept(['image/*'], 'image/png')).toBe(true);
    expect(matchAccept(['image/*'], 'application/pdf')).toBe(false);
    expect(matchAccept(['image/*', 'application/pdf'], 'application/pdf')).toBe(true);
    expect(matchAccept(['IMAGE/PNG'], 'image/png')).toBe(true);
    expect(matchAccept(['image/png'], 'image/jpeg')).toBe(false);
  });
  it('matches extensions and wildcards', () => {
    expect(matchAccept(['.pdf'], '', 'A.PDF')).toBe(true);
    expect(matchAccept(['*/*'], 'x/y')).toBe(true);
    expect(matchAccept(undefined, 'x/y')).toBe(true);
    expect(matchAccept([], 'x/y')).toBe(true);
  });
  it('checks type and size', () => {
    const spec = { accept: ['image/*'], max_size: '1KB' };
    expect(checkFile(spec, { type: 'image/png', name: 'a.png', size: 10 })).toBeNull();
    expect(checkFile(spec, { type: 'image/png', name: 'a.png', size: 2048 })).toBe('size');
    expect(checkFile(spec, { type: 'text/plain', name: 'a.txt', size: 1 })).toBe('type');
    expect(checkFile(spec, { type: '', name: 'a.png', size: 1 })).toBeNull();
  });
  it('picks the first accepting type, preferring the current one', () => {
    const specs = [
      { name: 'image', upload: { accept: ['image/*'] } },
      { name: 'document', upload: { accept: ['application/pdf'] } },
      { name: 'any' },
    ];
    expect(pickType(specs, { type: 'application/pdf', name: 'a.pdf', size: 1 })).toBe('document');
    expect(pickType(specs, { type: 'text/plain', name: 'a.txt', size: 1 })).toBe('any');
    expect(pickType(specs, { type: 'image/png', name: 'a.png', size: 1 }, 'any')).toBe('any');
    expect(pickType(specs.slice(0, 2), { type: 'text/plain', name: 'a.txt', size: 1 })).toBeUndefined();
  });
});

describe('sizes', () => {
  it('parses', () => {
    expect(parseSize(2048)).toBe(2048);
    expect(parseSize('20MB')).toBe(20 * 1024 * 1024);
    expect(parseSize('512 kb')).toBe(512 * 1024);
    expect(parseSize('1.5M')).toBe(1.5 * 1024 * 1024);
    expect(parseSize('100')).toBe(100);
    expect(parseSize('big')).toBeUndefined();
    expect(parseSize(undefined)).toBeUndefined();
  });
  it('formats', () => {
    expect(formatSize(12)).toBe('12 B');
    expect(formatSize(2048)).toBe('2 KB');
    expect(formatSize(1536)).toBe('1.5 KB');
    expect(formatSize(5 * 1024 * 1024)).toBe('5 MB');
  });
});

describe('previews', () => {
  it('picks a kind by mime', () => {
    expect(previewKind('image/svg+xml')).toBe('image');
    expect(previewKind('video/mp4')).toBe('video');
    expect(previewKind('audio/mpeg')).toBe('audio');
    expect(previewKind('application/pdf')).toBe('pdf');
    expect(previewKind('application/zip')).toBe('file');
    expect(previewKind(undefined)).toBe('file');
  });
  it('builds the public path and reads file values', () => {
    expect(assetPath({ id: 'ab', name: 'my photo.png' })).toBe('/assets/ab/my%20photo.png');
    expect(fileOf({ id: 'ab', name: 'x' })?.id).toBe('ab');
    expect(fileOf('x')).toBeUndefined();
    expect(fileOf({})).toBeUndefined();
    expect(dimensions({ width: 4, height: 3 })).toBe('4 × 3');
    expect(dimensions({})).toBe('');
  });
});

describe('focal point', () => {
  const rect = { left: 100, top: 50, width: 200, height: 100 };
  it('maps a pointer to 0–1 and clamps outside the image', () => {
    expect(focalFromPoint(rect, 200, 100)).toEqual({ x: 0.5, y: 0.5 });
    expect(focalFromPoint(rect, 100, 50)).toEqual({ x: 0, y: 0 });
    expect(focalFromPoint(rect, 999, -5)).toEqual({ x: 1, y: 0 });
    expect(focalFromPoint(rect, 250, 125)).toEqual({ x: 0.75, y: 0.75 });
    expect(focalFromPoint({ left: 0, top: 0, width: 0, height: 0 }, 1, 1)).toEqual({ x: 0.5, y: 0.5 });
  });
  it('steps with the arrow keys from the centre when unset', () => {
    expect(focalStep(undefined, 'ArrowRight')).toEqual({ x: 0.55, y: 0.5 });
    expect(focalStep({ x: 0.55, y: 0.5 }, 'ArrowDown')).toEqual({ x: 0.55, y: 0.55 });
    expect(focalStep({ x: 0.5, y: 0.5 }, 'ArrowLeft', true)).toEqual({ x: 0.49, y: 0.5 });
    expect(focalStep({ x: 1, y: 0 }, 'ArrowRight')).toEqual({ x: 1, y: 0 });
    expect(focalStep({ x: 1, y: 0 }, 'ArrowUp')).toEqual({ x: 1, y: 0 });
    expect(focalStep(undefined, 'a')).toBeUndefined();
  });
  it('keeps the point in view when cropped', () => {
    expect(objectPosition(undefined)).toBe('50% 50%');
    expect(objectPosition({ x: 0.25, y: 0.8 })).toBe('25% 80%');
    expect(clamp01(2)).toBe(1);
  });
});

describe('tags', () => {
  it('adds trimmed, unique names', () => {
    expect(addTags(['a'], ' b, a ,,c')).toEqual(['a', 'b', 'c']);
    expect(addTags([], '   ')).toEqual([]);
  });
});

describe('upload limit', () => {
  const f = { type: 'image/png', name: 'a.png', size: 20 };
  it('the smaller of the server limit and the type max applies', () => {
    expect(checkFile({ accept: ['image/*'] }, f, 10)).toBe('size');
    expect(checkFile({ accept: ['image/*'] }, f, 100)).toBeNull();
    expect(checkFile({ max_size: 10 }, f, 100)).toBe('size');
    expect(checkFile({ max_size: 100 }, f, 10)).toBe('size');
    expect(checkFile({ accept: ['application/pdf'] }, f, 10)).toBe('type');
  });
  it('names the reason when no type takes the file', () => {
    const specs = [{ name: 'image', upload: { accept: ['image/*'] } }];
    expect(pickType(specs, f, 'image', 10)).toBeUndefined();
    expect(whyRejected(specs, f, 10)).toBe('size');
    expect(whyRejected(specs, { ...f, type: 'application/pdf', name: 'a.pdf' }, 10)).toBe('type');
  });
});

describe('asset base', () => {
  it('serves from the configured base, a path or a URL', () => {
    expect(assetPath({ id: 'ab', name: 'a b.png' }, '/media/')).toBe('/media/ab/a%20b.png');
    expect(assetPath({ id: 'ab', name: 'a.png' }, 'https://cdn.example.com/x')).toBe('https://cdn.example.com/x/ab/a.png');
    expect(assetPath({ id: 'ab', name: 'a.png' }, '')).toBe('/assets/ab/a.png');
  });
});

describe('dropped files', () => {
  const file = (n: string) => new File(['x'], n);
  const item = (f: File | null, dir = false) => ({ kind: 'file', getAsFile: () => f, webkitGetAsEntry: () => ({ isDirectory: dir }) });
  it('skips directories and non-files', () => {
    const dt = { files: [] as unknown as FileList, items: [item(file('a.png')), item(file('dir'), true), { kind: 'string', getAsFile: () => null }] as unknown as DataTransferItemList };
    expect(droppedFiles(dt).map((x) => x.name)).toEqual(['a.png']);
  });
  it('falls back to files without items', () => {
    const dt = { files: [file('b.png')] as unknown as FileList, items: [] as unknown as DataTransferItemList };
    expect(droppedFiles(dt).map((x) => x.name)).toEqual(['b.png']);
  });
});
