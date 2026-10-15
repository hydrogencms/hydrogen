import { createHash } from 'node:crypto'

/** SHA-256 hex of the preview token: what `<meta name="hydrogen-preview">` carries (never the token). Own module: the proxy must not load node:crypto. */
export const previewHash = (token: string): string => createHash('sha256').update(token).digest('hex')
