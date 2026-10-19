import { hydrogen } from '$lib/server/hydrogen'

// GET /api/hydrogen/preview?token=…&path=/en/about keeps the token in an httpOnly cookie; ?exit=1 leaves.
export const GET = hydrogen.previewRoute
