import { hydrogen } from '$lib/server/hydrogen'

// Resolves the preview cookie and the channel per request, and hands `?preview=<token>` URLs to the preview route.
export const handle = hydrogen.handle
