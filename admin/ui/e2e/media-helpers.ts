import { deflateSync } from 'node:zlib';
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
