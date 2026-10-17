import { deflateSync, inflateSync } from 'node:zlib';
import { createHash } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { base } from './helpers';

const crcTable = Array.from({ length: 256 }, (_, n) => {
  let c = n;
  for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
  return c >>> 0;
});
const crc32 = (b: Buffer) => {
  let c = 0xffffffff;
  for (const x of b) c = crcTable[(c ^ x) & 0xff] ^ (c >>> 8);
  return (c ^ 0xffffffff) >>> 0;
};
const chunk = (type: string, data: Buffer) => {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([len, body, crc]);
};

/** A valid w x h RGB PNG filled with one colour; a distinct colour per run gives distinct bytes (and sha). */
export function makePNG(w = 4, h = 3, rgb: [number, number, number] = [200, 30, 30]): Buffer {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  const row = Buffer.concat([Buffer.from([0]), Buffer.from(Array.from({ length: w }, () => rgb).flat())]);
  const raw = Buffer.concat(Array.from({ length: h }, () => row));
  return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}

export const randomPNG = () => makePNG(4, 3, [Math.floor(Math.random() * 256), Math.floor(Math.random() * 256), Math.floor(Math.random() * 256)]);
export const sha256 = (b: Buffer) => createHash('sha256').update(b).digest('hex');
// A trailing comment makes the bytes unique, so the upload is not deduplicated onto an earlier run's asset.
export const tinyPDF = (tag: string) => Buffer.concat([readFileSync(new URL('./fixtures/files/tiny.pdf', import.meta.url)), Buffer.from(`%${tag}\n`)]);

/** Absolute URL for an asset `url` (delivery may return a path or an absolute URL). */
export const assetURL = (u: string) => (/^https?:/.test(u) ? u : new URL(u, base).toString());

/** A w x h RGB PNG whose top half is `top` and bottom half `bottom`; the colours make the bytes unique per run. */
export function makeSplitPNG(w: number, h: number, top: [number, number, number], bottom: [number, number, number]): Buffer {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(w, 0);
  ihdr.writeUInt32BE(h, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  const rowOf = (c: [number, number, number]) => Buffer.concat([Buffer.from([0]), Buffer.from(Array.from({ length: w }, () => c).flat())]);
  const raw = Buffer.concat(Array.from({ length: h }, (_, y) => rowOf(y < h / 2 ? top : bottom)));
  return Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw)), chunk('IEND', Buffer.alloc(0))]);
}

/** The RGB of pixel (x, y) of an 8-bit RGB or RGBA PNG with any scanline filter. */
export function pngPixel(b: Buffer, x: number, y: number): [number, number, number] {
  const { width, height } = pngSize(b);
  const type = b[25];
  if (b[24] !== 8 || (type !== 2 && type !== 6)) throw new Error(`unsupported PNG (depth ${b[24]}, colour type ${type})`);
  const bpp = type === 2 ? 3 : 4;
  const idat: Buffer[] = [];
  for (let o = 8; o < b.length; ) {
    const len = b.readUInt32BE(o);
    if (b.subarray(o + 4, o + 8).toString() === 'IDAT') idat.push(b.subarray(o + 8, o + 8 + len));
    o += 12 + len;
  }
  const raw = inflateSync(Buffer.concat(idat));
  const stride = width * bpp;
  const out = Buffer.alloc(stride * height);
  for (let r = 0; r < height; r++) {
    const f = raw[r * (stride + 1)];
    for (let i = 0; i < stride; i++) {
      const v = raw[r * (stride + 1) + 1 + i];
      const a = i >= bpp ? out[r * stride + i - bpp] : 0;
      const up = r > 0 ? out[(r - 1) * stride + i] : 0;
      const c = i >= bpp && r > 0 ? out[(r - 1) * stride + i - bpp] : 0;
      let add = 0;
      if (f === 1) add = a;
      else if (f === 2) add = up;
      else if (f === 3) add = (a + up) >> 1;
      else if (f === 4) {
        const pa = Math.abs(up - c), pb = Math.abs(a - c), pc = Math.abs(a + up - 2 * c);
        add = pa <= pb && pa <= pc ? a : pb <= pc ? up : c;
      }
      out[r * stride + i] = (v + add) & 0xff;
    }
  }
  const at = y * stride + x * bpp;
  return [out[at], out[at + 1], out[at + 2]];
}

/** Width and height from a PNG's IHDR chunk. */
export function pngSize(b: Buffer): { width: number; height: number } {
  if (b.length < 24 || b.subarray(1, 4).toString() !== 'PNG' || b.subarray(12, 16).toString() !== 'IHDR') throw new Error('not a PNG');
  return { width: b.readUInt32BE(16), height: b.readUInt32BE(20) };
}
