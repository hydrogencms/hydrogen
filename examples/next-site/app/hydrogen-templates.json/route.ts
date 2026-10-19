import { hydrogen } from '@/lib/hydrogen'

// Prerendered at build time; scripts/write-templates.mjs copies it to hydrogen-templates.json for
// `hydrogen check-frontend -templates`.
export const GET = () => Response.json({ templates: hydrogen.templates() })
