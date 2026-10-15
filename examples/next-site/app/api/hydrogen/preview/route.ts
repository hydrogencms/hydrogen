import { hydrogen } from '@/lib/hydrogen'

// GET /api/hydrogen/preview?token=…&path=/en/about enables draft mode; ?exit=1 leaves it.
export const GET = hydrogen.previewRoute()
