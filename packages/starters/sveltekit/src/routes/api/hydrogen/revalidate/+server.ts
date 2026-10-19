import { hydrogen } from '$lib/server/hydrogen'

// Target of the CMS webhook (events "*"): verifies the signature and purges the event's cache tags.
export const POST = hydrogen.revalidate
